#!/bin/sh
set -eu

capture_dir="${1:-data/sync-captures}"
mkdir -p "$capture_dir"
echo "正在监控同步详情：$capture_dir"
echo "请在网页重新同步商品；捕获后按 Ctrl+C 停止。"

known=""
while :; do
  current=$(find "$capture_dir" -maxdepth 1 -type f -name '*.meta.json' -print 2>/dev/null | sort)
  for file in $current; do
    case "\n$known\n" in
      *"\n$file\n"*) ;;
      *)
        echo "捕获成功：$file"
        sed -n '1,160p' "$file"
        known="${known}${known:+
}$file"
        ;;
    esac
  done
  sleep 1
done
