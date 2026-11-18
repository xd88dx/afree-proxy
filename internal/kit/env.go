package kit

import (
	"fmt"
	"os"
	"strings"
)

// envBool 环境变量布尔解析：1/true/yes/y/on（大小写不敏感）为真，其余
// （含未设置/空白）为假。
func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}

// UnableForce 全冷却兜底开关（环境变量 UNABLE_FORCE，默认 false）。
//
//   - true：某平台凭据全部处于冷却时，继续按各平台既有的兜底实现选用冷却中
//     的凭据（保持请求流动）；各平台的扩展实现全部保留——workbuddy 的软冷却
//     模型豁免与「最早到期优先」、zen 的 live 会话优先等。
//   - false（默认）：全冷却一律拒绝——冷却凭据绝不参与路由，请求以统一的
//     「no <platform> ... routable: all configured ... are cooling,
//     routing-disabled or bound-exit-blocked」错误失败（2026-10-28 用户定）。
//
// 每次调用实时读环境变量（os.Getenv 开销可忽略），不缓存：容器 env 变更后
// 重启即生效，也无测试隔离问题。
func UnableForce() bool { return envBool("UNABLE_FORCE") }

// DisableDirect 直连禁用开关（环境变量 DISABLE_DIRECT，默认 false）。
//
//   - true：出口解析结果为直连的凭据一律视为不可路由——绑定直连哨兵的凭据
//     在隔离候选过滤阶段即被剔除（pickBoundProxy 返回出口不可用）；未绑定
//     凭据在全局规则为直连（平台代理开关关闭、或代理池空兜底）时同样失效。
//     「当前选择了直连的凭据全部失效」（2026-10-28 用户定）。
//   - false（默认）：与既有行为相同，直连是合法出口。
func DisableDirect() bool { return envBool("DISABLE_DIRECT") }

// OnlyRouter 仅会话路由开关（环境变量 ONLY_ROUTER，默认 false）。
//
//   - true：所有平台只保留会话路由（chat 请求）与其必要支撑（token 续期、
//     模型目录同步、积分倍率预热等请求路径依赖），签到/保活/余额巡检/补领
//     清扫等一切维护任务不启动——面板上的 autoCheckin 等开关被本变量强制
//     覆盖（2026-10-28 用户定）。
//   - false（默认）：维护任务按各自配置照常运行。
func OnlyRouter() bool { return envBool("ONLY_ROUTER") }

func EnvInt(key string) (int, bool) {
	v := EnvStr(key)
	if v == "" {
		return 0, false
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			fmt.Printf("  WARNING: unrecognized integer %q for %s, using default\n", v, key)
			return 0, false
		}
		n = n*10 + int(c-'0')
		// 溢出保护：手写的 n*10+d 没有上界，MAX_BODY_MB=99999999999999999999
		// 会回绕成负数，<< 20 之后得到一个荒谬甚至为负的上限。环境变量是
		// 运维输入，算错了要按"无效"回退默认，而不是悄悄产生一个反向的保护。
		if n > maxEnvInt {
			fmt.Printf("  WARNING: integer %q for %s exceeds %d, using default\n", v, key, maxEnvInt)
			return 0, false
		}
	}
	return n, true
}

// maxEnvInt 环境变量整数的上限（1e9）。所有消费者都是 MB/分钟/次数这类量级，
// 超过它一定是笔误，按无效处理回退默认。
const maxEnvInt = 1_000_000_000

func EnvStr(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func EnvBool(key string) (bool, bool) {
	v := strings.ToLower(EnvStr(key))
	if v == "" {
		return false, false
	}
	switch v {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	}
	fmt.Printf("  WARNING: unrecognized boolean %q for %s, using default\n", v, key)
	return false, false
}
