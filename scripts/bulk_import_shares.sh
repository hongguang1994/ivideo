#!/usr/bin/env bash
#
# bulk_import_shares.sh —— 批量往 OpenList 灌"分享挂载"的 DEMO
#
# 演示小雅那种"一次性批量创建挂载"的做法:读一个 CSV 清单 → 循环调
# OpenList 的 storage/create 接口。
#
# ⚠️ 默认 DRY_RUN=1(空跑):只打印"将要发送的请求",不真正写入 OpenList。
#    确认无误后再用 DRY_RUN=0 真跑。示例 CSV 里全是假数据,直接跑不会污染。
#
# 用法:
#   ./bulk_import_shares.sh                      # 空跑,读默认 shares.example.csv
#   CSV=my.csv ./bulk_import_shares.sh           # 指定清单
#   DRY_RUN=0 CSV=my.csv ./bulk_import_shares.sh # 真正写入
#
# CSV 格式(逗号分隔,# 开头为注释,空行忽略):
#   driver,mount_path,f1,f2,f3
#   - aliyun 行: aliyun,挂载名,share_id,share_pwd,root_folder_id
#   - 115 行:   115,挂载名,share_code,receive_code,root_folder_id
#
set -euo pipefail

# ---- 配置(按需用环境变量覆盖)----------------------------------------
OPENLIST_URL="${OPENLIST_URL:-http://127.0.0.1:5244}"
OPENLIST_USER="${OPENLIST_USER:-admin}"
OPENLIST_PASS="${OPENLIST_PASS:-ivideo123}"
CSV="${CSV:-$(dirname "$0")/shares.example.csv}"
DRY_RUN="${DRY_RUN:-1}"                 # 1=空跑(默认)  0=真写入
# 阿里分享要用到你自己账号的 refresh_token;空跑时留空即可。
ALIYUN_REFRESH_TOKEN="${ALIYUN_REFRESH_TOKEN:-<你的阿里refresh_token>}"
# 115 分享的 cookie(选填,可留空匿名浏览)。
P115_COOKIE="${P115_COOKIE:-}"
# ---------------------------------------------------------------------

log()  { printf '\033[36m[demo]\033[0m %s\n' "$*"; }
warn() { printf '\033[93m[warn]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[91m[err ]\033[0m %s\n' "$*" >&2; exit 1; }

command -v jq >/dev/null || die "需要 jq(用于拼 JSON),请先安装"
[[ -f "$CSV" ]] || die "找不到清单文件: $CSV"

# ---- 登录拿 token(空跑时跳过,不联网)-------------------------------
TOKEN=""
login() {
  if [[ "$DRY_RUN" == "1" ]]; then
    log "空跑模式:跳过登录(不连接 $OPENLIST_URL)"
    TOKEN="DRYRUN_FAKE_TOKEN"
    return
  fi
  log "登录 $OPENLIST_URL ..."
  TOKEN="$(curl -fsS "$OPENLIST_URL/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg u "$OPENLIST_USER" --arg p "$OPENLIST_PASS" \
          '{username:$u, password:$p}')" \
    | jq -r '.data.token // empty')"
  [[ -n "$TOKEN" ]] || die "登录失败,检查用户名/密码"
  log "登录成功"
}

# ---- 把一行 CSV 拼成 storage/create 的 addition(JSON 字符串)---------
build_addition() {
  local driver="$1" f1="$2" f2="$3" f3="$4"
  case "$driver" in
    aliyun|AliyundriveShare)
      jq -nc \
        --arg sid "$f1" --arg pwd "$f2" \
        --arg root "${f3:-root}" --arg rt "$ALIYUN_REFRESH_TOKEN" \
        '{share_id:$sid, share_pwd:$pwd, root_folder_id:$root, refresh_token:$rt}'
      ;;
    115|115Share)
      jq -nc \
        --arg sc "$f1" --arg rc "$f2" \
        --arg root "${f3:-0}" --arg ck "$P115_COOKIE" \
        '{share_code:$sc, receive_code:$rc, root_folder_id:$root, cookie:$ck}'
      ;;
    *) die "未知 driver: $driver(支持 aliyun / 115)";;
  esac
}

driver_name() {
  case "$1" in
    aliyun|AliyundriveShare) echo "AliyundriveShare";;
    115|115Share)            echo "115 Share";;
    *) die "未知 driver: $1";;
  esac
}

# ---- 创建单个存储 ----------------------------------------------------
create_storage() {
  local mount_path="$1" driver="$2" addition="$3"
  local body
  body="$(jq -nc \
    --arg mp "$mount_path" --arg dv "$driver" --arg add "$addition" \
    '{mount_path:$mp, driver:$dv, addition:$add,
      order:0, cache_expiration:30,
      web_proxy:false, webdav_policy:"302_redirect"}')"

  if [[ "$DRY_RUN" == "1" ]]; then
    log "将创建: mount_path=$mount_path  driver=$driver"
    printf '       POST %s/api/admin/storage/create\n' "$OPENLIST_URL"
    printf '       body: %s\n' "$(echo "$body" | jq -c '.addition |= (fromjson | .refresh_token //= "***" | .cookie //= "" | tojson)')"
    return
  fi

  local resp code
  resp="$(curl -fsS "$OPENLIST_URL/api/admin/storage/create" \
    -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
    -d "$body")"
  code="$(echo "$resp" | jq -r '.code')"
  if [[ "$code" == "200" ]]; then
    log "✅ 已创建: $mount_path"
  else
    warn "❌ 失败: $mount_path -> $(echo "$resp" | jq -r '.message')"
  fi
}

# ---- 主流程 ----------------------------------------------------------
main() {
  log "清单文件: $CSV"
  log "目标:     $OPENLIST_URL"
  [[ "$DRY_RUN" == "1" ]] && warn "DRY_RUN=1 空跑,不会写入任何数据(改 DRY_RUN=0 才真写)"
  login

  local n=0
  while IFS=, read -r driver mount_path f1 f2 f3 || [[ -n "$driver" ]]; do
    # 跳过注释和空行
    [[ -z "${driver// }" || "${driver:0:1}" == "#" ]] && continue
    driver="${driver// }"; mount_path="${mount_path// }"
    local real_driver addition
    real_driver="$(driver_name "$driver")"
    addition="$(build_addition "$driver" "${f1:-}" "${f2:-}" "${f3:-}")"
    create_storage "$mount_path" "$real_driver" "$addition"
    n=$((n+1))
  done < "$CSV"

  log "完成,共处理 $n 条。"
}

main "$@"
