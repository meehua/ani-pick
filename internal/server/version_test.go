package server

import "testing"

func TestServerBuildID(t *testing.T) {
	cases := []struct {
		version, commit, want string
	}{
		{"0.1.2", "4f2a9c1", "0.1.2-4f2a9c1"},
		{"0.1.2", "none", "0.1.2"}, // 本地裸构建：无提交哈希，退化为仅版本号
		{"0.1.2", "", "0.1.2"},
		{"dev", "none", "dev"},
	}
	for _, c := range cases {
		s := &Server{version: c.version, commit: c.commit}
		if got := s.buildID(); got != c.want {
			t.Errorf("buildID(version=%q commit=%q) = %q, want %q", c.version, c.commit, got, c.want)
		}
	}
}
