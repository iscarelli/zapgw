# HANDOFF — zapgw

**Origem:** `DownServer`, 2026-09-01 17:10 -0300
**Repo:** `iscarelli/zapgw` (branch `main`) — 🔴 **PÚBLICO**
**Base:** `7410119` — *T-216: aviso de nome obsoleto agora aparece no deploy que da certo*
**Produção:** `v0.64.0`, medida no `/v1/health` no deploy (`VERSAO CONFERE`)

> **Leia antes:** `CLAUDE.md` (regras duras + verify), e o **bloco de retomada no topo de
> `docs/TASKS.md`**, que é a fonte do estado. Este arquivo é o resumo; lá está o detalhe.

---

## O que estávamos fazendo

Duas frentes, as duas fechadas nesta sessão:

**1. A migração do contrato para inglês (T-189).** O gateway falava português no JSON com os
consumidores. Migrou em quatro passos, sem uma mensagem perdida — e a prova não é a suíte: é a
medição do `consumer-b` contra produção, em que `"observed"` chega e vira `"observado"` nos sete
pontos de leitura dele **sem uma linha de código dele ter mudado**.

**2. A limpeza do PT-BR do resto do sistema**, decidida pelo dono. Quatro camadas, medidas antes de
tocar, porque o risco de cada uma é completamente diferente.

---

## Estado atual

### Pronto e em produção

- **Contrato em inglês** — ~96 chaves de saída e 12 vocabulários de valor. `cru`/`payload` intocados,
  byte a byte.
- **Camada 1** — 86 arquivos `.go` renomeados (`git mv`, histórico preservado) e 38 identificadores.
- **Camada 4** — 16 variáveis `ZAPGW_*` e 4 verbos de CLI aceitam os dois nomes; o novo vence; o
  arranque **avisa** quando o velho foi usado.
- **Seis portões**, e cada um **reprovou contra dado real antes de ser confiado**:
  nome de cliente · telefone no intervalo empurrado · dentro de commit de merge · mensagem de tag ·
  contrato de saída · ponteiro morto em doc.

### Medido, esperando decisão

- **Camada 3** — `docs/INVENTARIO-STRINGS.md`. ~44 mensagens só na resposta, **~132 em AMBOS**
  (resposta **e** log — só de `Request.Validate()` são 103). **Total que chega ao consumidor: ~176.**
  🔴 Dez delas vivem em respostas de **sucesso** (`NextStep`, `resp.Warning`, `resp.Message`), não em
  erro — quem varrer só `respondError` não acha.
- **Camada 2** — os **18 nomes de contador** continuam em português **de propósito**. O par não foi
  decidido, e o `consumer-b` **alarma em 8 deles**.

---

## Próximos passos, em ordem

1. **Ler o canal antes de qualquer coisa.** O arquivo que o consumidor escreve é
   `<raiz>/<consumidor-b>-STATUS.local.md` (gitignorado). **Há uma pergunta pendente lá**, feita em
   2026-09-01 09:46: *eles comparam o TEXTO de uma mensagem de erro, ou só `classe` e `codigo_meta`?*
   A resposta decide se traduzir as ~176 é faxina ou quebra de contrato.
2. **`/etc/zapgw/env` de produção** usa **seis nomes obsoletos** (o deploy agora os imprime):
   `ENTRADA_VIA`, `CHAVE_CIFRA`, `BANCO`, `ENDERECO`, `CONECTOR_READY`, `SONDA_EXTERNA_URL`.
   🔴 **É do dono** — o arquivo guarda a chave de cifra, e a sessão não toca em arquivo de segredo.
3. **Os pares dos 18 contadores** — decisão do dono, **com o consumidor no circuito**.
4. **Apagar o apelido de ENTRADA** — autorizado pelo dono, **mas o portão é medição**: só com
   `nome_antigo_usado` em **zero** *e* um contador de **volume** subindo no mesmo período.

---

## Pegadinhas / decisões — não refaça isto

- 🔴 **O repositório é PÚBLICO e o histórico dele não se reescreve.** Nome de cliente, telefone, id
  de terceiro e endereço interno **não entram**. O `pre-push` bloqueia antes de sair da máquina — ele
  já pegou o planner **duas vezes**, nas duas escrevendo justamente sobre essa regra.
- 🔴 **A tabela é a fonte.** `docs/MIGRACAO-CONTRATO-EN.md` (+ par pt-BR). *O que não está nela não
  muda* — foi concluindo o contrário que o consumidor quebrou o upload de mídia.
- 🔴 **Campo da Meta continua com o nome da Meta, e isso vale para SUBESTRUTURA** (seção 10). Um
  componente de template é um objeto inteiro dela aninhado num nosso. Ignorar isso custou, do lado do
  consumidor, 76 templates sem botão e uma cobrança enviada sem o botão do Pix.
- 🔴 **Isenção é por CAMINHO, nunca por palavra.** Banir a palavra `"tipo"` de uma lista para calar um
  falso positivo legítimo cegou a varredura do contrato para a chave mais importante dele.
- 🔴 **"Não consegui verificar" ≠ "está limpo".** Todo portão aqui falha fechado, e `go test` vindo
  `(cached)` **não é resultado** — use `-count=1` em qualquer controle.
- 🔴 **Contador só conta o que tem par publicado.** Chave sem par é invisível para ele. Antes de um
  número autorizar qualquer coisa, pergunte **o que ele não consegue ver**.
- **Deploy:** `bash ~/.zapgw/deploy-zapgw.sh` (fora do repo; lê a topologia do repo privado antigo).
  A prova é a linha `VERSAO CONFERE`, que compara a versão construída com a que o gateway responde.
- **Monitor do canal:** não se arma sozinho — **o dono manda**. Nesta sessão ele mandou, e a demora
  em armá-lo custou cinco horas de uma resposta parada.
- **Sem agente em voo, o planner commita na hora e não empurra** enquanto houver um; `git add` é por
  caminho, e **`git commit -am` é o irmão do `add -A`** — já varreu trabalho não commitado aqui.
