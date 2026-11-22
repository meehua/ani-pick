// 抓取错误的界面文案：给管理员一句人话，原始报错只做折叠的详情内容。
// 上游错误是自由文本，这里按常见形态归类；未识别的形态给通用兜底。

const NETWORK = /dial tcp|connect|refused|timed? ?out|timeout|deadline|eof|tls|ssl|certificate|proxy|network|no such host|lookup .*: no|i\/o error|broken pipe|reset by peer/
const RATE = /429|rate limit|too many requests/
const AUTH = /401|unauthorized|forbidden|403|credential|凭据|token/
const MISSING = /404|not found|不存在/
const UNCONFIGURED = /未配置|not configured|api ?key/

/** 把一条抓取报错翻译成界面人话（`label` 是来源展示名）。 */
export function humanizeCrawlError(label: string, err: string): string {
  const e = err.toLowerCase()
  if (UNCONFIGURED.test(e)) return `还没有配置 ${label} 需要的凭据：在该来源卡片的展开器里填写后重试`
  if (NETWORK.test(e)) return `连不上 ${label}：网络不可达或被墙，可在下方「出站代理」里配置出口后重试`
  if (RATE.test(e)) return `${label} 暂时限流：请求太快被上游拒绝，稍后再试`
  if (AUTH.test(e)) return `${label} 的凭据无效或已过期`
  if (MISSING.test(e)) return `${label} 的接口地址已失效`
  return `${label} 抓取时出现问题`
}
