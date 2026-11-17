package app

import "testing"

func TestCanEditPost(t *testing.T) {
	cases := []struct {
		name     string
		u        authedUser
		authorID int64
		want     bool
	}{
		{"admin 对自己的文章", authedUser{ID: 1, Role: "admin"}, 1, true},
		{"admin 对别人的文章", authedUser{ID: 1, Role: "admin"}, 2, true},
		{"author 对自己的文章", authedUser{ID: 2, Role: "author"}, 2, true},
		{"author 对别人的文章", authedUser{ID: 2, Role: "author"}, 3, false},
		// 未认证用户(ID=0)不属于任何文章
		{"匿名对任何文章", authedUser{}, 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := canEditPost(c.u, c.authorID); got != c.want {
				t.Fatalf("canEditPost(%+v, %d) = %v, want %v", c.u, c.authorID, got, c.want)
			}
		})
	}
}
