package kit

// 原子写文件（2026-11-17 结构重构 P3b 从 app/global_config.go 迁入）：跨包
// 通用落盘原语——各平台/代理域配置与状态文件的 tmp+rename 原子写共用。

import (
	"log"
	"os"
	"path/filepath"
	"sync"
)

// atomicWriteMu 按路径互斥：固定临时名在两类场景下都会出事——同路径并发写
// 时第二个 rename 会 Access denied（Windows），或者 temp 与 rename 之间另一
// 写者已换名（生产实证 zen-state.json 两次 rename 失败）；即便临时名唯一，
// Windows 上并发 rename 覆盖同一目标也会 Access denied。按路径加互斥锁后两
// 者都消除。
var atomicWriteMu sync.Map // path -> *sync.Mutex

// AtomicWriteFile 原子写:先写临时文件再 rename,崩溃中途写入不会截断原文件。
// 临时文件每调用唯一命名，配合上面的按路径互斥，同路径并发写安全。
func AtomicWriteFile(path string, data []byte) {
	m, _ := atomicWriteMu.LoadOrStore(path, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		log.Printf("write %s: %v", path, err)
		return
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		log.Printf("write %s: %v", tmp, err)
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("replace %s: %v", path, err)
		_ = os.Remove(tmp)
	}
}
