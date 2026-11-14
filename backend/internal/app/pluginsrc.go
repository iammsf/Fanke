package app

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// 插件脚本的 TypeScript 支持。
//
// 约定：
//   - 插件包内可以直接放 .ts 源文件，安装时自动编译成同名 .js，两者都会落盘
//     （.ts 作为源码保留，运行时永远加载 .js）；
//   - index.html 里引用 .ts 的 script/link 会被自动改写为 .js；
//   - 对于已安装但缺少 .js 的插件，请求 .js 时按需从同名 .ts 编译并缓存，避免必须重装。

// transpileTS 只做类型擦除（不打包、不解析 import 路径），适合插件这种自包含脚本。
func transpileTS(src []byte, name string) ([]byte, error) {
	res := api.Transform(string(src), api.TransformOptions{
		Loader:        api.LoaderTS,
		Target:        api.ES2020,
		Format:        api.FormatDefault,
		Sourcefile:    name,
		Sourcemap:     api.SourceMapNone,
		LegalComments: api.LegalCommentsNone,
	})
	if len(res.Errors) > 0 {
		e := res.Errors[0]
		where := name
		if e.Location != nil {
			where = name + ":" + strconv.Itoa(e.Location.Line) + ":" + strconv.Itoa(e.Location.Column)
		}
		return nil, errors.New(where + " " + e.Text)
	}
	return res.Code, nil
}

// rewriteTSRefs 把 index.html 里指向 .ts 的资源引用改成 .js。
var tsRefRe = regexp.MustCompile(`((?:src|href)\s*=\s*["'])([^"']+)\.ts(["'])`)

func rewriteTSRefs(html []byte) []byte {
	return tsRefRe.ReplaceAll(html, []byte("${1}${2}.js${3}"))
}

// compileExtensionSources 处理安装阶段的 TS：编译 .ts → .js，并改写 index.html 引用。
func compileExtensionSources(files map[string][]byte) error {
	for name, data := range files {
		if !strings.HasSuffix(strings.ToLower(name), ".ts") {
			continue
		}
		js, err := transpileTS(data, name)
		if err != nil {
			return err
		}
		files[name[:len(name)-3]+".js"] = js
	}
	if idx, ok := files["index.html"]; ok {
		files["index.html"] = rewriteTSRefs(idx)
	}
	return nil
}

// ensureExtensionJS 运行时兜底：请求 /ext/<slug>/x.js 但磁盘只有 x.ts 时，现场编译并落盘。
func ensureExtensionJS(dir, rel string) (string, bool) {
	if !strings.HasSuffix(strings.ToLower(rel), ".js") {
		return "", false
	}
	jsPath, err := safeJoin(dir, rel)
	if err != nil {
		return "", false
	}
	if fi, err := os.Stat(jsPath); err == nil && !fi.IsDir() {
		return jsPath, false
	}
	tsRel := rel[:len(rel)-3] + ".ts"
	tsPath, err := safeJoin(dir, tsRel)
	if err != nil {
		return "", false
	}
	src, err := os.ReadFile(tsPath)
	if err != nil {
		return "", false
	}
	out, err := transpileTS(src, tsRel)
	if err != nil {
		return "", false
	}
	if err := os.MkdirAll(filepath.Dir(jsPath), 0o755); err != nil {
		return "", false
	}
	if err := os.WriteFile(jsPath, out, 0o644); err != nil {
		return "", false
	}
	return jsPath, true
}
