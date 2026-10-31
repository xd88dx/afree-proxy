package kit

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveDataPath 数据文件路径解析：DATA_DIR 环境变量优先（容器部署用），
// 其次 data/ 子目录（可执行文件目录，其次工作目录），兼容历史根目录存放；
// 均不存在时默认写到 data/ 子目录并自动创建目录。
// go run 运行时编译产物在临时目录，此时应回退到工作目录（项目根）的 data/。
func ResolveDataPath(filename string) string {
	// DATA_DIR 显式指定时始终使用它（容器场景 volume 挂载点）
	if dir := strings.TrimSpace(os.Getenv("DATA_DIR")); dir != "" {
		os.MkdirAll(dir, 0755)
		return filepath.Join(dir, filename)
	}
	exeDir, pwd := "", ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		pwd = wd
	}
	candidates := []string{}
	if exeDir != "" {
		candidates = append(candidates, filepath.Join(exeDir, "data", filename))
	}
	if pwd != "" {
		candidates = append(candidates, filepath.Join(pwd, "data", filename))
	}
	if exeDir != "" {
		candidates = append(candidates, filepath.Join(exeDir, filename))
	}
	if pwd != "" {
		candidates = append(candidates, filepath.Join(pwd, filename))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	// 均不存在：默认写到可执行文件目录 data/（go run 场景回退工作目录 data/），
	// 账号数据统一收敛在 data/ 下，避免散落项目根目录。
	if exeDir != "" {
		os.MkdirAll(filepath.Join(exeDir, "data"), 0755)
		return filepath.Join(exeDir, "data", filename)
	}
	os.MkdirAll(filepath.Join(pwd, "data"), 0755)
	return filepath.Join(pwd, "data", filename)
}

// DataDir 数据目录本身（供需要"列出目录内容"的场景，如配置指纹清单的
// glob 收集）。判定与 ResolveDataPath 的默认落点一致：DATA_DIR 优先，
// 其次可执行文件目录 data/，再次工作目录 data/。
func DataDir() string {
	if dir := strings.TrimSpace(os.Getenv("DATA_DIR")); dir != "" {
		return dir
	}
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "data"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "data"))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "data"
}

// FileExists 判断文件是否存在。
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
