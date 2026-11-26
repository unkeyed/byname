// Command genbaseline writes the workspace track's baseline: every table
// and view a workspace owns, with their indexes, constraints and seed
// rows, as the global migrations leave them in public. It migrates a
// scratch database on the docker compose Postgres, dumps the workspace
// tables with that server's pg_dump, and drops the schema qualifiers, so
// the file builds them in whichever schema search_path names first.
//
//	make workspace-baseline
//
// TestWorkspaceBaselineMatchesPublic fails when the file and the global
// migrations disagree.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unkeyed/byname/internal/db"
)

const defaultURL = "postgres://byname:byname@localhost:5432/byname?sslmode=disable"

const header = `-- The workspace baseline: every table and view a workspace owns, as the
-- global migrations leave them in public, with their seed rows. Generated
-- by make workspace-baseline; never edit it.
-- Migrations apply it with search_path set to the workspace's schema and
-- then public, so its references to shared tables, such as users and
-- credential, reach public's.
`

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genbaseline <output.sql>")
		os.Exit(2)
	}
	if err := run(context.Background(), os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "genbaseline:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, out string) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = defaultURL
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return err
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	scratch := "byname_baseline_" + hex.EncodeToString(b)

	admin, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		return fmt.Errorf("connect to the docker compose Postgres (docker compose up -d --wait db): %w", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+scratch); err != nil {
		return err
	}
	defer admin.Exec(context.WithoutCancel(ctx), `DROP DATABASE IF EXISTS `+scratch+` WITH (FORCE)`)

	cfg.ConnConfig.Database = scratch
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.MigrateThrough(ctx, pool, ""); err != nil {
		return err
	}

	tables, err := workspaceRelations(ctx, pool)
	if err != nil {
		return err
	}
	schema, err := dump(ctx, cfg.ConnConfig.User, scratch, tables)
	if err != nil {
		return err
	}
	seeds, err := seedRows(ctx, pool, tables)
	if err != nil {
		return err
	}
	sql := header + "\n" + schema + seeds
	if m := regexp.MustCompile(`(?i)\bpublic\b`).FindString(sql[len(header):]); m != "" {
		return fmt.Errorf("the dump still names public, so a workspace would read the first workspace's tables")
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, []byte(sql), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Clean(out))
}

func workspaceRelations(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p', 'v', 'm', 'f') ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, s := range db.SharedTables {
		if !slices.Contains(all, s) {
			return nil, fmt.Errorf("db.SharedTables names %s, which the migrations do not create", s)
		}
	}
	return slices.DeleteFunc(all, func(t string) bool { return slices.Contains(db.SharedTables, t) }), nil
}

// dump runs the compose server's own pg_dump, so its output matches the
// server, and keeps only the DDL: no session settings, no comments, and no
// schema names.
func dump(ctx context.Context, user, database string, tables []string) (string, error) {
	args := []string{"compose", "exec", "-T", "db", "pg_dump", "-U", user, "-d", database,
		"--schema-only", "--no-owner", "--no-privileges", "--no-comments", "--no-tablespaces",
		"--no-security-labels", "--no-publications", "--no-subscriptions"}
	for _, t := range tables {
		args = append(args, "-t", "public."+t)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pg_dump: %w: %s", err, stderr.String())
	}
	var out strings.Builder
	blank := true
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "--"), strings.HasPrefix(line, `\`),
			strings.HasPrefix(line, "SET "), strings.HasPrefix(line, "SELECT pg_catalog.set_config"):
			continue
		case strings.TrimSpace(line) == "":
			if !blank {
				out.WriteString("\n")
			}
			blank = true
			continue
		}
		blank = false
		out.WriteString(strings.ReplaceAll(line, "public.", "") + "\n")
	}
	return strings.Trim(out.String(), "\n") + "\n", sc.Err()
}

func seedRows(ctx context.Context, pool *pgxpool.Pool, tables []string) (string, error) {
	var out strings.Builder
	for _, t := range tables {
		var cols []string
		var identity bool
		err := pool.QueryRow(ctx, `SELECT coalesce(array_agg(quote_ident(a.attname) ORDER BY a.attnum)
				FILTER (WHERE a.attgenerated = '' AND coalesce(pg_get_expr(d.adbin, d.adrelid), '') <> 'now()'), '{}'),
			coalesce(bool_or(a.attidentity <> ''), false)
			FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
			WHERE a.attrelid = ('public.' || quote_ident($1))::regclass AND a.attnum > 0 AND NOT a.attisdropped
			AND (SELECT relkind FROM pg_class WHERE oid = a.attrelid) IN ('r', 'p')`, t).Scan(&cols, &identity)
		if err != nil {
			return "", err
		}
		if len(cols) == 0 {
			continue
		}
		quoted := make([]string, len(cols))
		for i, c := range cols {
			quoted[i] = "quote_nullable(" + c + ")"
		}
		rows, err := pool.Query(ctx, `SELECT concat_ws(', ', `+strings.Join(quoted, ", ")+`) AS v
			FROM public.`+pgx.Identifier{t}.Sanitize()+` ORDER BY v`)
		if err != nil {
			return "", err
		}
		values, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return "", err
		}
		if len(values) == 0 {
			continue
		}
		if identity {
			return "", fmt.Errorf("the migrations seed %s, which has an identity column; teach genbaseline to carry its sequence", t)
		}
		fmt.Fprintf(&out, "\nINSERT INTO %s (%s) VALUES\n", t, strings.Join(cols, ", "))
		for i, v := range values {
			sep := ","
			if i == len(values)-1 {
				sep = ";"
			}
			fmt.Fprintf(&out, "    (%s)%s\n", v, sep)
		}
	}
	return out.String(), nil
}
