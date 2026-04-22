# Orbit — Demo session

> **Este arquivo é a sessão REAL capturada com o binário v0.1.1**, pronto para
> virar GIF/asciicast no topo do README. Instruções de gravação ao final.

---

## O que você verá em ~15 segundos

```
$ curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash

🛰  orbit install

[1/5] detectando plataforma...              ✓  linux-amd64
[2/5] resolvendo versão...                   ✓  v0.1.1
[3/5] baixando orbit-v0.1.1-linux-amd64...  ✓  binário + .sha256 baixados
[4/5] verificando sha256...                  ✓  integridade confirmada
[5/5] instalando em ~/.orbit/bin/orbit...   ✓  orbit version v0.1.1 (commit=0317dae)

✅  orbit instalado com sucesso

   Próximo passo (10s):
     ~/.orbit/bin/orbit quickstart
```

```
$ orbit quickstart

[1/3] Inicializando sessão...
      ✓  session_id=qs-1776867921809708189
[2/3] Executando: echo hello
      → hello
      ✓  evento registrado   event_id=00ebba83bb5b...
      ✓  tokens=42   proof=299c672a2cad92e7...
[3/3] Verificando proof...
      ✓  proof válido (sha256 verificado)
      ✨ proof generated

✅  Quickstart concluído! Orbit está funcionando.
```

```
$ orbit run go test ./...
(executa go test, registra exit code + output + snapshot do git)

$ orbit verify ~/.orbit/logs/*.json | tail -1
    session: run-... · ts: 2026-04-22T... · bytes: 1243

✅  proof confere
```

---

## Como gravar o GIF

Este repo **não versiona GIFs binários** (`TestNoLargeBinariesTracked` bloqueia
qualquer tracked > 5 MiB). Recomendação: use **asciinema** (texto), converta pra
GIF só para a landing.

```bash
# 1. Instalar asciinema
brew install asciinema      # macOS
# ou: pipx install asciinema

# 2. Gravar em terminal limpo (container ou VM isolada)
asciinema rec orbit-demo.cast

# Dentro da gravação, rode exatamente:
#   curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash
#   ~/.orbit/bin/orbit quickstart
#   ~/.orbit/bin/orbit run echo hello
#   ~/.orbit/bin/orbit verify ~/.orbit/logs/*.json | tail -3
# Ctrl+D para fechar

# 3. Upload público (não versiona no repo)
asciinema upload orbit-demo.cast
# → gera URL https://asciinema.org/a/XXXXX

# 4. Embed no README:
#    [![asciicast](https://asciinema.org/a/XXXXX.svg)](https://asciinema.org/a/XXXXX)
```

Para **GIF** (se preferir):
```bash
# Converte .cast → .gif usando agg (asciinema gif generator)
brew install agg
agg orbit-demo.cast orbit-demo.gif --theme monokai --font-size 14
# Upload em CDN externo (imgur, cloudinary) — NÃO versionar no repo
# Embed: ![demo](https://cdn.exemplo.com/orbit-demo.gif)
```

---

## Checklist pré-gravação

- [ ] Tag `v0.1.1` está em `git ls-remote --tags origin` (senão `curl | bash` falha em [2/5])
- [ ] GitHub Releases tem binários `orbit-v0.1.1-{linux,darwin}-{amd64,arm64}` + `.sha256`
- [ ] `make release-gate VERSION=v0.1.1` retorna 🟢 PASS no seu terminal real
- [ ] Terminal limpo sem `~/.orbit/` prévio e `PATH` sem `orbit` já instalado
