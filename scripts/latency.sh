#!/usr/bin/env bash
# Вимір p50/p95 для Lambda. Потрібні змінні: URL (без кінцевого /) і MCP_TOKEN.
# Запуск: URL=https://... MCP_TOKEN=... ./scripts/latency.sh
set -euo pipefail
: "${URL:?задай URL}" "${MCP_TOKEN:?задай MCP_TOKEN}"

M='{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}'
LIST="{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\",\"params\":{\"_meta\":$M}}"
ARGS='{"from":{"name":"EasyPost","street1":"417 Montgomery Street","city":"San Francisco","state":"CA","zip":"94104","country":"US","phone":"4153334445"},"to":{"name":"Dr. Steve Brule","street1":"179 N Harbor Dr","city":"Redondo Beach","state":"CA","zip":"90277","country":"US","phone":"8573875756"},"parcel":{"length":10,"width":8,"height":4,"weight":32}}'
CALL="{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"get_rates\",\"arguments\":$ARGS,\"_meta\":$M}}"

# t <метод> <тіло> [ще curl-аргументи]: друкує лише час запиту в секундах
t() { m=$1; b=$2; shift 2
  curl -s -o /dev/null -w '%{time_total}\n' -X POST "$URL/mcp" \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -H 'MCP-Protocol-Version: 2026-07-28' -H "Mcp-Method: $m" \
    -H "Authorization: Bearer $MCP_TOKEN" -d "$b" "$@"; }

# nearest-rank: k = ceil(p*N), k-й елемент після sort -n
p() { sort -n "$1" | awk -v p="$2" '{a[NR]=$1} END {k=int(p*NR); if (k<p*NR) k++; print a[k]}'; }

echo "cold start (перший виклик; найчесніше після паузи 15+ хв):"
# перший виклик меряємо разом з кодом: 401 дає швидкі «гарні» числа, тож зупиняємось
out=$(curl -s -o /dev/null -w '%{http_code} %{time_total}' -X POST "$URL/mcp" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' \
  -H "Authorization: Bearer $MCP_TOKEN" -d "$LIST")
[ "${out%% *}" = 200 ] || { echo "tools/list дав HTTP ${out%% *}, вимір зупинено"; exit 1; }
COLD=${out##* }; echo "  $COLD"

for i in $(seq 50); do t tools/list "$LIST"; done > list.txt
for i in $(seq 30); do t tools/call "$CALL" -H 'Mcp-Name: get_rates'; done > rates.txt

echo
echo "| операція | N | p50 | p95 | cold start |"
echo "|---|---|---|---|---|"
echo "| tools/list | $(wc -l < list.txt | tr -d ' ') | $(p list.txt 0.5) | $(p list.txt 0.95) | $COLD |"
echo "| get_rates | $(wc -l < rates.txt | tr -d ' ') | $(p rates.txt 0.5) | $(p rates.txt 0.95) | |"
