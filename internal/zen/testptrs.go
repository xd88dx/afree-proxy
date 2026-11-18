package zen

// 测试白盒指针访问（2026-11-17 P3d）：跨包测试经这些函数拿到内部真身指针，
// 与包内小写变量同实例。生产代码不得使用。

import (
	"sync"
	"time"
)

func ZenConfigMuPtr() *sync.Mutex                   { return &zenConfigMu }
func ZenConfigOverridePtr() *string                 { return &zenConfigPathOverride }
func ZenUsageOverridePtr() *string                  { return &zenUsagePathOverride }
func ZenFileMuPtr() *sync.Mutex                     { return &zenFileMu }
func ZenSessMuPtr() *sync.Mutex                     { return &zenSessMu }
func ZenSessionsPtr() *map[string]*ZenSessionEntry  { return &zenSessions }
func ZenSessLoadedPtr() *bool                       { return &zenSessLoaded }
func ZenSessSaveBlockedPtr() *bool                  { return &zenSessSaveBlocked }
func ZenModelsMuPtr() *sync.RWMutex                 { return &zenModelsMu }
func ZenModelsPtr() *map[string]*ZenModel           { return &zenModels }
func ZenKeyMuPtr() *sync.Mutex                      { return &zenKeyMu }
func ZenKeyIdxPtr() *int                            { return &ZenKeyIdx }
func ZenKeyCoolPtr() *map[string]time.Time          { return &zenKeyCool }
func ZenKeyTokensPtr() *map[string]ZenKeyTokenStats { return &zenKeyTokens }
func ZenKeyParkPtr() *map[string]ZenParkState       { return &zenKeyPark }
func ZenGraceModelsPtr() *map[string]ZenGraceEntry  { return &zenGraceModels }
func ZenDepMuPtr() *sync.Mutex                      { return &DepMu }
func ZenDepAliasesPtr() *map[string]*ZenDepEntry    { return &DepAliasesVar }
func ZenOrgMuPtr() *sync.Mutex                      { return &zenOrgMu }
func ZenOrgDataPtr() *ZenOrgStore                   { return &zenOrgData }
func ZenOrgLoadedPtr() *bool                        { return &zenOrgLoaded }
func ZenOrgFlightPtr() *map[string]*sync.WaitGroup  { return &zenOrgFlight }
func ZenTokensFlushPtr() *interface{ Mark() }       { return nil } // 占位：flusher 经 Host
func ZenSessFailedMuPtr() *sync.Mutex               { return &zenSessFailedMu }
func ZenSessFailedPtr() *map[string]int             { return &zenSessFailed }
func ZenStateMuPtr() *sync.Mutex                    { return &zenStateMu }
func ZenFailCountPtr() *int                         { return &zenFailCount }
func ZenFailUntilPtr() *time.Time                   { return &zenFailUntil }
func ZenEndpointSaveMuPtr() *sync.Mutex             { return &zenEndpointSaveMu }
func ZenNativeUAPtr() *string                       { return &zenNativeUA }
func ZenModelPrefixPtr() *string                    { return &ZenModelPrefix }

func ZenConfigPathOverride() string     { return zenConfigPathOverride }
func SetZenConfigPathOverride(v string) { zenConfigPathOverride = v }

func ZenUsagePathOverride() string     { return zenUsagePathOverride }
func SetZenUsagePathOverride(v string) { zenUsagePathOverride = v }
