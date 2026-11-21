package sources

import (
	"net/netip"
	"testing"
)

func TestRobots(t *testing.T) {
	const txt = `# comment
User-agent: Googlebot
Disallow: /

User-agent: *
Disallow: /admin
Allow: /admin/public
Disallow: /*.pdf$
Disallow: /tmp*/cache

User-agent: Other
User-agent: BYNAME
Disallow: /drafts
Disallow:
`
	mine := parseRobots(txt, robotsToken)
	star := parseRobots("User-agent: *\nDisallow: /admin\nAllow: /admin/public\nDisallow: /*.pdf$\nDisallow: /tmp*/cache\n", robotsToken)
	for _, c := range []struct {
		r    robots
		path string
		want bool
	}{
		{mine, "/drafts/x", false},
		{mine, "/admin", true}, // a group naming byname replaces the * group
		{star, "/admin/x", false},
		{star, "/admin/public/x", true}, // the longer Allow wins
		{star, "/paper.pdf", false},
		{star, "/paper.pdf?x=1", true}, // $ anchors the end
		{star, "/tmp1/cache/a", false},
		{star, "/", true},
		{allowAll, "/anything", true},
		{disallowAll, "/anything", false},
	} {
		if got := c.r.allowed(c.path); got != c.want {
			t.Errorf("allowed(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"93.184.216.34": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fe80::1": false, "fd00::1": false, "::ffff:127.0.0.1": false,
		"64:ff9b::a00:1": false, "224.0.0.1": false,
	} {
		if got := publicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("publicAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}
