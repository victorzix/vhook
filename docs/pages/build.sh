#!/usr/bin/env bash
# Injeta docs/pages/_style.css no lugar do marcador /*__STYLE__*/ de cada página.
#
# A CSP dos Artifacts bloqueia stylesheet externo, então o CSS precisa estar
# inline no HTML publicado. Este script mantém uma fonte só: edite o .css e
# rode isto, em vez de editar o mesmo bloco em N arquivos.
#
# Uso: bash docs/pages/build.sh

set -euo pipefail
cd "$(dirname "$0")/../.."

STYLE="docs/pages/_style.css"
PAGES=(docs/overview.html)

for page in "${PAGES[@]}"; do
  python -c "
import re, sys, io
style = io.open('$STYLE', encoding='utf-8').read().strip()
html  = io.open('$page', encoding='utf-8').read()
new, n = re.subn(r'<style>.*?</style>', '<style>\n' + style + '\n</style>', html, count=1, flags=re.S)
if n == 0:
    sys.exit('sem bloco <style> em $page')
io.open('$page', 'w', encoding='utf-8', newline='\n').write(new)
print('  ok $page')
"
done

echo "css injetado em ${#PAGES[@]} arquivo(s)"
