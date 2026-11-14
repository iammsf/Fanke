package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"fanke/backend/internal/render"
	"fanke/backend/internal/repo"
)

func testLifecycleDispatcher(exts []repo.Extension, handlers map[string]map[string]LifecycleHandler) lifecycleDispatcher {
	return lifecycleDispatcher{list: func(context.Context) ([]repo.Extension, error) { return slices.Clone(exts), nil }, handlers: handlers}
}

func TestLifecycleFilteringAndOrder(t *testing.T) {
	var called []int64
	fn := func(_ context.Context, ext repo.Extension, _ *LifecycleData) error {
		called = append(called, ext.ID)
		return nil
	}
	exts := []repo.Extension{
		{ID: 9, Slug: "last", Enabled: true, Initialized: true, Hooks: []string{BeforePostCreate}},
		{ID: 2, Slug: "disabled", Initialized: true, Hooks: []string{BeforePostCreate}},
		{ID: 3, Slug: "uninitialized", Enabled: true, Hooks: []string{BeforePostCreate}},
		{ID: 4, Slug: "undeclared", Enabled: true, Initialized: true},
		{ID: 5, Slug: "no-handler", Enabled: true, Initialized: true, Hooks: []string{BeforePostCreate}},
		{ID: 1, Slug: "first", Enabled: true, Initialized: true, Hooks: []string{BeforePostCreate, BeforePostCreate}},
	}
	handlers := map[string]map[string]LifecycleHandler{}
	for _, slug := range []string{"last", "disabled", "uninitialized", "undeclared", "first"} {
		handlers[slug] = map[string]LifecycleHandler{BeforePostCreate: fn}
	}
	d := testLifecycleDispatcher(exts, handlers)
	if err := d.dispatch(context.Background(), BeforePostCreate, &LifecycleData{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(called, []int64{1, 9}) {
		t.Fatalf("called %v", called)
	}
}

func TestLifecycleRejectAndAfterError(t *testing.T) {
	for _, event := range []string{BeforePostCreate, BeforePostUpdate, BeforePostDelete, BeforeMediaUpload, BeforeMediaDelete, AfterPostCreated, AfterPostUpdated, AfterPostDeleted, AfterMediaUploaded, AfterMediaDeleted} {
		t.Run(event, func(t *testing.T) {
			var called []string
			d := testLifecycleDispatcher([]repo.Extension{
				{ID: 99, Slug: "a-reject", Enabled: true, Initialized: true, Hooks: []string{event}},
				{ID: 1, Slug: "next", Enabled: true, Initialized: true, Hooks: []string{event}},
			}, map[string]map[string]LifecycleHandler{
				"a-reject": {event: func(context.Context, repo.Extension, *LifecycleData) error {
					called = append(called, "reject")
					return errors.New("rejected")
				}},
				"next": {event: func(context.Context, repo.Extension, *LifecycleData) error {
					called = append(called, "next")
					return nil
				}},
			})
			err := d.dispatch(context.Background(), event, &LifecycleData{})
			if strings.HasPrefix(event, "before") {
				if err == nil || len(called) != 1 {
					t.Fatalf("before: %v, %v", err, called)
				}
			} else if err != nil || len(called) != 2 {
				t.Fatalf("after reversed success or stopped: %v, %v", err, called)
			}
		})
	}
}

func TestLifecycleListFailure(t *testing.T) {
	d := lifecycleDispatcher{list: func(context.Context) ([]repo.Extension, error) { return nil, errors.New("database unavailable") }}
	for event := range lifecycleEvents {
		err := d.dispatch(context.Background(), event, &LifecycleData{})
		if strings.HasPrefix(event, "before") != (err != nil) {
			t.Fatalf("%s: %v", event, err)
		}
	}
}

func TestLifecycleSnapshotsAndContentOnly(t *testing.T) {
	cover := int64(7)
	date := time.Now().UTC()
	in := repo.PostInput{Title: "original", CoverMediaID: &cover, PublishedAt: &date, Categories: []repo.TermRef{{Name: "category"}}, ContentHTML: "original"}
	old := repo.Post{Title: "old", CoverMediaID: &cover, Terms: []repo.Term{{Name: "term"}}}
	data := &LifecycleData{Post: &in, OldPost: &old, ContentHTML: in.ContentHTML, Media: &repo.Media{Key: "original"}, Variants: []repo.MediaVariant{{Key: "variant"}}}
	d := testLifecycleDispatcher([]repo.Extension{{ID: 1, Slug: "mutator", Enabled: true, Initialized: true, Hooks: []string{BeforePostUpdate}}}, map[string]map[string]LifecycleHandler{
		"mutator": {BeforePostUpdate: func(_ context.Context, _ repo.Extension, d *LifecycleData) error {
			d.Post.Title = "changed"
			*d.Post.CoverMediaID = 99
			*d.Post.PublishedAt = time.Time{}
			d.Post.Categories[0].Name = "changed"
			d.OldPost.Terms[0].Name = "changed"
			*d.OldPost.CoverMediaID = 100
			d.OldPost.Title = "changed"
			d.Media.Key = "changed"
			d.Variants[0].Key = "changed"
			d.ContentHTML = "allowed"
			return nil
		}},
	})
	if err := d.dispatch(context.Background(), BeforePostUpdate, data); err != nil {
		t.Fatal(err)
	}
	if data.ContentHTML != "allowed" || in.ContentHTML != "original" || in.Title != "original" || cover != 7 || date.IsZero() || in.Categories[0].Name != "category" || old.Terms[0].Name != "term" || old.Title != "old" || data.Media.Key != "original" || data.Variants[0].Key != "variant" {
		t.Fatalf("snapshot isolation broken: %+v, %+v", data, in)
	}
}

func TestPreparePostSaveNormalizeThenSanitize(t *testing.T) {
	for _, event := range []string{BeforePostCreate, BeforePostUpdate} {
		for _, source := range []string{
			`<img src="/safe.jpg" des="caption" onerror="evil()">说明<script>evil()</script>`,
			render.SanitizeHTML(`<img src="/safe.jpg" des="caption">说明</img>`),
			`<img src="/safe.jpg">说明</img>`,
		} {
			for _, markdown := range []bool{false, true} {
				input := source
				if markdown {
					var err error
					input, err = renderPostMarkdown(source)
					if err != nil {
						t.Fatal(err)
					}
				}
				in := repo.PostInput{ContentHTML: input}
				d := testLifecycleDispatcher([]repo.Extension{{ID: 1, Slug: "image-viewer", Enabled: true, Initialized: true, Hooks: []string{event}}}, lifecycleRegistry.handlers)
				if err := preparePostSave(context.Background(), event, 1, &in, nil, d.dispatch); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(in.ContentHTML, `说明</img>`) {
					t.Fatalf("lost plugin image: %q", in.ContentHTML)
				}
				// 带 des 的标签必须保留说明属性；普通闭合图片不强求补 des_pos(默认位置改由渲染时套用)
				if strings.Contains(source, `des="caption"`) && !strings.Contains(in.ContentHTML, `des="caption"`) {
					t.Fatalf("lost des attribute: %q", in.ContentHTML)
				}
				for _, bad := range []string{"onerror", "<script", "evil()"} {
					if strings.Contains(in.ContentHTML, bad) {
						t.Fatalf("unsafe HTML: %q", in.ContentHTML)
					}
				}
			}
		}
	}
}

func TestPreparePostSaveRejectLeavesInput(t *testing.T) {
	in := repo.PostInput{Title: "original", ContentHTML: `<script>unsafe</script>`}
	original := in
	d := lifecycleDispatcher{list: func(context.Context) ([]repo.Extension, error) { return nil, errors.New("load failed") }}
	writes := 0
	if err := preparePostSave(context.Background(), BeforePostCreate, 0, &in, nil, d.dispatch); err == nil {
		writes++
	}
	if writes != 0 || !reflect.DeepEqual(in, original) {
		t.Fatal("rejection entered persistence or changed input")
	}
}

func TestLifecycleNotCallableCapability(t *testing.T) {
	a := &App{}
	for event := range lifecycleEvents {
		if len(ValidateHooks([]string{event})) != 0 || !slices.Contains(HookNames(), event) {
			t.Fatalf("event not recognized: %s", event)
		}
		if lookupHook(event) != nil {
			t.Fatalf("event exposed as capability: %s", event)
		}
		for _, public := range []bool{false, true} {
			if _, err := a.InvokeHook(context.Background(), &repo.Extension{Hooks: []string{event}}, event, nil, public); err == nil {
				t.Fatalf("manual lifecycle accepted: %s", event)
			}
		}
	}
	if !reflect.DeepEqual(ValidateHooks([]string{"not-an-event"}), []string{"not-an-event"}) {
		t.Fatal("accepted unknown hook")
	}
}
