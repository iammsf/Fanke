package repo

import "time"

type User struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	Username     string     `json:"username"`
	Role         string     `json:"role"`
	DisplayName  string     `json:"display_name"`
	AvatarURL    string     `json:"avatar_url"`
	Bio          string     `json:"bio"`
	CreatedAt    time.Time  `json:"created_at"`
	PwdChangedAt *time.Time `json:"-"`
	LockedUntil  *time.Time `json:"-"`
	FailedCount  int        `json:"-"`
	PasswordHash string     `json:"-"`
}

type Post struct {
	ID           int64      `json:"id"`
	Slug         string     `json:"slug"`
	Title        string     `json:"title"`
	Summary      string     `json:"summary"`
	ContentMD    string     `json:"content_md,omitempty"`
	ContentHTML  string     `json:"content_html,omitempty"`
	Status       string     `json:"status"`
	AuthorID     int64      `json:"author_id"`
	AuthorName   string     `json:"author_name,omitempty"`
	CoverMediaID *int64     `json:"cover_media_id"`
	CoverURL     string     `json:"cover_url,omitempty"`
	CoverWidth   int        `json:"cover_width,omitempty"`  // 封面原始宽(前端写 width 属性,消除 CLS)
	CoverHeight  int        `json:"cover_height,omitempty"` // 封面原始高
	AllowComments bool      `json:"allow_comments"`
	IsPublic      bool      `json:"is_public"`
	Views        int64      `json:"views"`
	Type         string     `json:"type"`  // standard / moment
	Sticky       int        `json:"sticky"` // 0=非置顶;>0 置顶权重,小的靠前
	PublishedAt  *time.Time `json:"published_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Terms        []Term     `json:"terms,omitempty"`
}

// PostNavItem 表示文章详情页上一篇/下一篇导航项的最小公开字段。
type PostNavItem struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

type Term struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Type string `json:"type"`
}

type Media struct {
	ID        int64     `json:"id"`
	Filename  string    `json:"filename"`
	Storage   string    `json:"storage"`
	Key       string    `json:"key"`
	URL       string    `json:"url"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

type Comment struct {
	ID          int64      `json:"id"`
	PostID      int64      `json:"post_id"`
	ParentID    *int64     `json:"parent_id"`
	AuthorName  string     `json:"author_name"`
	AuthorEmail string     `json:"author_email,omitempty"`
	UserID      *int64     `json:"user_id"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	IPHash      string     `json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
}
