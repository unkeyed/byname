package db

// WorkspaceLock is the SQL for the advisory lock key named name in the
// workspace the connection is bound to, so work in one workspace never
// waits on or skips the same work in another. The first workspace, the
// one whose schema is not ws_<id>, keeps hashtext(name), the key releases
// before workspaces took, so the old and new copies running side by side
// during a deploy still keep each other out. name is a constant such as
// byname.checks.claim.
func WorkspaceLock(name string) string {
	return `hashtext(CASE WHEN current_schema() ~ '^ws_[0-9]+$' THEN '` + name + `:' || current_schema() ELSE '` + name + `' END)`
}
