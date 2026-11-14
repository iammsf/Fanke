package app

import (
	"net/http"
	"strconv"
	"sync"
)

// inflightSet 记录"某用户正在执行的某个写请求",用于拒绝重复提交。
// 前端已用全屏忙碌遮罩禁止键鼠,这里只做后端兜底:同一用户对同一路径(+)的
// 写请求在未完成前再次到达,直接 409,避免两次 DB 写入。
type inflightSet struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

func newInflightSet() *inflightSet {
	return &inflightSet{keys: make(map[string]struct{})}
}

// acquire 返回 true 表示拿到锁;false 表示已有相同请求在执行。
func (s *inflightSet) acquire(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.keys[key]; busy {
		return false
	}
	s.keys[key] = struct{}{}
	return true
}

func (s *inflightSet) release(key string) {
	s.mu.Lock()
	delete(s.keys, key)
	s.mu.Unlock()
}

// dedupeWrites 对管理端写方法做并发去重(POST/PUT/PATCH/DELETE)。
func (a *App) dedupeWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}
		u := currentUser(r)
		key := strconv.FormatInt(u.ID, 10) + " " + r.Method + " " + r.URL.Path
		if !a.Limits.Writes.acquire(key) {
			a.fail(w, http.StatusConflict, "请求正在处理,请勿重复提交")
			return
		}
		defer a.Limits.Writes.release(key)
		next.ServeHTTP(w, r)
	})
}
