package sources

// CredStore 是数据源凭据的运行期快照：管理设置里的凭据（如 TMDB API Key）
// 经 server 在启动与保存设置后注入（Set），适配器热路径无锁读取（Get）。
// 凭据不再走环境变量——全部来自后台设置，secretbox 加密存库。

import "sync/atomic"

type CredStore struct {
	v atomic.Value // map[string]map[string]string（source → field → value）
}

// NewCredStore 构造空快照。
func NewCredStore() *CredStore {
	c := &CredStore{}
	c.Set(nil)
	return c
}

// Set 整体替换凭据快照（浅拷贝一层防调用方改坏内部状态）。
func (c *CredStore) Set(all map[string]map[string]string) {
	out := make(map[string]map[string]string, len(all))
	for src, fields := range all {
		f := make(map[string]string, len(fields))
		for k, v := range fields {
			f[k] = v
		}
		out[src] = f
	}
	c.v.Store(out)
}

// Get 读一个凭据字段值；来源/字段未配置返回空串。
func (c *CredStore) Get(source, field string) string {
	m, _ := c.v.Load().(map[string]map[string]string)
	return m[source][field]
}
