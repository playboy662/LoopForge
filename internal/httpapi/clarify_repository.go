package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// One scoped read tool gives every CLI repository context without enabling
// shell or edit tools on the control plane. os.Root also confines symlinks.
func clarifyRepositoryTool() agentToolDescriptor {
	return agentToolDescriptor{
		Name: "clarify_repository_read", Kind: "clarify-repository",
		Description: "只读分析仓库。path 为相对路径，默认 .；目录返回文件列表，文件返回带行号的文本；query 非空时递归搜索文件名和文本。startLine 可继续读取文件。结果有大小限制，可缩小 path 或 query。",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"query":{"type":"string"},"startLine":{"type":"integer","minimum":1}},"additionalProperties":false}`),
	}
}

func readClarifyRepository(ctx context.Context, dir string, args json.RawMessage) (string, error) {
	var input struct {
		Path      string `json:"path"`
		Query     string `json:"query"`
		StartLine int    `json:"startLine"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}
	if input.Path == "" {
		input.Path = "."
	}
	if !fs.ValidPath(input.Path) {
		return "", fmt.Errorf("path 必须是分析仓库内的相对路径")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	var out strings.Builder
	visited := 0
	truncated := false
	write := func(text string) {
		if out.Len()+len(text) > 32000 {
			truncated = true
			return
		}
		out.WriteString(text)
	}
	read := func(path string) error {
		info, err := root.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("仅支持普通文本文件")
		}
		file, err := root.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err = file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("仅支持普通文本文件")
		}
		data, err := io.ReadAll(io.LimitReader(file, 1024*1024))
		if err != nil {
			return err
		}
		if strings.ContainsRune(string(data), 0) {
			return nil
		}
		if input.Query != "" && strings.Contains(path, input.Query) {
			write(path + "\n")
		}
		for i, line := range strings.Split(string(data), "\n") {
			if ctx.Err() != nil || truncated {
				break
			}
			if i+1 < input.StartLine || (input.Query != "" && !strings.Contains(line, input.Query)) {
				continue
			}
			write(fmt.Sprintf("%s:%d: %s\n", path, i+1, line))
		}
		if input.Query == "" && info.Size() > int64(len(data)) {
			truncated = true
		}
		return nil
	}
	if input.Query != "" {
		err = fs.WalkDir(root.FS(), input.Path, func(path string, entry fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if truncated || visited >= 2000 {
				truncated = true
				return fs.SkipAll
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "node_modules" {
					return fs.SkipDir
				}
				return nil
			}
			visited++
			if entry.Type().IsRegular() {
				return read(path)
			}
			return nil
		})
	} else {
		var info fs.FileInfo
		info, err = root.Stat(input.Path)
		if err == nil && info.IsDir() {
			var entries []fs.DirEntry
			entries, err = fs.ReadDir(root.FS(), input.Path)
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() {
					name += "/"
				}
				write(name + "\n")
			}
		} else if err == nil {
			err = read(input.Path)
		}
	}
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if truncated {
		out.WriteString("\n结果已截断，请缩小范围或使用 startLine 继续。")
	}
	return out.String(), nil
}
