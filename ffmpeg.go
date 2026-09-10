package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	ffmpegOnce     sync.Once
	ffmpegBin      string
	ffplayBin      string
	ffprobeBin     string
	ffmpegTestDirs []string
)

func ffmpegBinary() string {
	resolveFFmpegTools()
	if ffmpegBin != "" {
		return ffmpegBin
	}
	return "ffmpeg"
}

func ffplayBinary() string {
	resolveFFmpegTools()
	if ffplayBin != "" {
		return ffplayBin
	}
	return "ffplay"
}

func ffprobeBinary() string {
	resolveFFmpegTools()
	if ffprobeBin != "" {
		return ffprobeBin
	}
	return "ffprobe"
}

func resolveFFmpegTools() {
	ffmpegOnce.Do(func() {
		ffmpegBin, _ = findFFmpegTool("ffmpeg")
		ffplayBin, _ = findFFmpegTool("ffplay")
		ffprobeBin, _ = findFFmpegTool("ffprobe")
	})
}

func ffmpegToolExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func findFFmpegTool(name string) (string, error) {
	fileName := name + ffmpegToolExt()
	for _, dir := range ffmpegSearchDirs() {
		path := filepath.Join(dir, fileName)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return exec.LookPath(name)
}

func ffmpegSearchDirs() []string {
	if len(ffmpegTestDirs) > 0 {
		return append([]string{}, ffmpegTestDirs...)
	}
	var dirs []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		exeDir := filepath.Dir(exe)
		add(filepath.Join(exeDir, "ffmpeg"))
		add(filepath.Join(exeDir, "resources", "ffmpeg"))
		add(exeDir)
	}
	if cwd, err := os.Getwd(); err == nil {
		add(filepath.Join(cwd, "resources", "ffmpeg"))
		add(filepath.Join(cwd, "ffmpeg"))
	}
	return dirs
}
