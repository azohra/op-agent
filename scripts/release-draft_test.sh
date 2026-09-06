#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/repo/scripts" "$scratch/bin"
cp "$root/mise.toml" "$scratch/repo/"
printf 'git rev-parse HEAD\n' > "$scratch/repo/scripts/release-source.sh"
cd "$scratch/repo"
git init -q -b main
git config user.name test
git config user.email test@example.invalid
git add .
git -c commit.gpgsign=false commit -qm fixture
git -c tag.gpgsign=false tag v1.2.3
git remote add origin https://github.com/azohra/op-agent.git
cat > "$scratch/bin/gh" <<'GH'
#!/bin/sh
case "$SCENARIO" in
 draft) printf true ;;
 published) printf false ;;
 missing) echo 'HTTP 404: not found' >&2; exit 1 ;;
 forbidden) echo 'HTTP 403: forbidden' >&2; exit 1 ;;
esac
GH
printf '#!/bin/sh\ntouch "$PUBLISH_CALLED"\n' > "$scratch/bin/goreleaser"
chmod +x "$scratch/bin/gh" "$scratch/bin/goreleaser"
export PATH="$scratch/bin:$PATH" GITHUB_TOKEN=fixture PUBLISH_CALLED="$scratch/called"
for scenario in missing draft; do
 SCENARIO="$scenario" mise run --skip-deps release -- v1.2.3 >/dev/null 2>&1
 [ -f "$PUBLISH_CALLED" ]
 rm "$PUBLISH_CALLED"
done
for scenario in forbidden published; do
 if SCENARIO="$scenario" mise run --skip-deps release -- v1.2.3 >/dev/null 2>&1; then exit 1; fi
 [ ! -f "$PUBLISH_CALLED" ]
done
echo 'Release draft: new and incomplete allowed; published and lookup failures refused'
