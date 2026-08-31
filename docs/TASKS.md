# Tasks

## Em voo — retomar por aqui

> Bloco de retomada. **Cada item sai daqui quando for resolvido**; ele nao e' historico.
> Escrito ao fim de 2026-08-30, o dia em que o repositorio virou publico. Bloco de retomada
> mentindo e' pior que bloco nenhum: e' o primeiro texto que a proxima sessao le.

✅ **FEITO EM 2026-08-31 00:44 (-03; `gh repo view --json createdAt` = `03:44:28Z`): o repositorio
publico foi APAGADO E RECRIADO, e o historico comeca
num commit so.** Medido, nao afirmado:
- `git rev-list --count origin/main` = **1**. A arvore do commit genesis e' **identica** a que passou
  no verify (`git diff` entre a antiga HEAD e o genesis: vazio).
- **As 8 agulhas dao zero** na arvore e no `origin`. O portao de nome (T-193) e' o que mede isso
  agora — nao mais um `git grep` de quem lembrou.
- **Release `v0.60.1` reposto e PROVADO byte a byte:** baixei de volta do release novo e o `sha256`
  dos dois binarios bate com o do release original (`a48a031d…` amd64, `a32a78e4…` arm64).
- **Segredo `ZAPGW_FORBIDDEN_NAMES` criado** no repositorio novo, entregue por `stdin` a partir de
  `~/.zapgw/forbidden-names.txt` — nunca em linha de comando.
- **O que a recriacao levou e nao volta:** o historico anterior (15 commits), as issues e as
  estrelas (eram zero). Nada funcional apontava para o GitHub — varri `implanta/` e `.github/`: so' um
  `Documentation=` na unit do systemd, e a URL nao mudou.

✅ **FEITO (T-195): a CI recebe o segredo `ZAPGW_FORBIDDEN_NAMES` como `env:` de JOB e ganhou passo
proprio do portao de nome** (`.github/workflows/verify.yml`), espelhando o portao de telefone.
✅ **E ela RODOU: verde as 00:54, com os quatro portoes passando** (run `33355320803`). Os **tres
runs anteriores falharam** no portao de nome, por falta de agulha — entao a propria CI ja reprovou
contra dado real antes de ser confiada, que e' o criterio desta casa.
🔴 **E isso derrubou uma coisa que eu tinha acabado de escrever aqui:** *"a cota de Actions so' reseta
em 2026-09-01"*. Os runs executaram em **08-31**. A data veio da explicacao do dono e virou estado
sem ninguem re-medir. *Afirmacao sobre coisa que voce nao controla envelhece calada* — e esta e' a
**terceira** vez que este mesmo paragrafo mente, agora registrado no `CLAUDE.md`.
⚠️ **Decisao que eu tomei e que voce pode querer rever:** num PR vindo de **fork**, o GitHub nao
entrega segredo, entao o portao vai reprovar por "nao consegui verificar" — e eu escolhi manter
assim, falhando fechado, em vez de virar skip. Skip seria a cegueira que o portao existe para nao ter.
Documentado em comentario no proprio workflow.

✅ **A `v0.61.0` ESTA EM PRODUCAO desde 2026-08-31 10:19, e o consumidor foi liberado as 10:21.**
Prova medida, nao afirmada: `SAUDE OK: {"ok":true,"versao":"0.61.0"}` seguido de
`VERSAO CONFERE: 0.61.0 (igual a construida)`, uma troca de binario atomica com o mesmo `sha256`
conferido no no e dentro do container, saida `0`.
- **O passo 3 (escritores deles em ingles) esta liberado.** Nao ha janela para acertar: o gateway
  aceita os dois idiomas ao mesmo tempo, e vai aceitar ate o passo 4.
- 📌 **O deploy roda por `~/.zapgw/deploy-zapgw.sh`**, fora do repositorio. Ele LE os cinco valores de
  topologia do `deploy.sh` do repo privado antigo (`/c/dev/zapgw-dev/implanta/deploy.sh:53-57`), onde
  eles ficaram como default do alvo real — o publico passou a EXIGI-los porque endereco interno nao
  entra aqui. **Nenhum valor passa por chat, commit ou linha de comando.**
- ⚠️ **Licao do proprio deploy:** o runner extraia o valor da chave SSH com `sed`, entao o `$HOME`
  saia LITERAL — o ssh avisou 21 vezes que nao achava a chave, caiu no agente, **e o deploy funcionou
  assim mesmo**. *Falha que ainda entrega o resultado certo e' a que ninguem conserta*, e 21 avisos
  por execucao ensinam a ignorar a saida do deploy, que e' onde mora a prova. Consertado.
✅ **A TAG `v0.61.0` ESTA NO `origin`**, apontando para o commit do bump (`6f975f4`). O portao a
recusava por falso positivo — tag que aponta para commit ja publicado acrescenta um *ponteiro*, nao
commits, e ele lia zero como "medicao vazia". **T-204 consertou distinguindo as duas causas**, e
acrescentou o que faltava: a **MENSAGEM da tag anotada e' varrida**, sempre. Provado com agulha real
na mensagem — bloqueia citando `mensagem da tag, linha 1`.

📌 **O passo 4 e' MAJOR e PARA PARA PERGUNTAR AO DONO.** Ele vira a saida para ingles e depois apaga o
apelido de entrada. Nao acontece sozinho, aconteca o que acontecer com a fila.

✅ **FECHADO: o par ANTES/DEPOIS existe, e a `v0.60.1` passou.** Medicao do consumidor em
2026-08-31 00:28: **77 segundos contra 79 do ANTES**, mesmo roteiro e mesmo template,
`tentativas: 1` em tudo, nenhuma retentativa. A assimetria de status que ficou aberta no ANTES sumiu
— `sent`, `delivered` e `read` nos dois disparos —, **sem concluir que consertamos nada**: pode ser
ordem de chegada da Meta, e eles disseram isso em vez de creditar a versao.
⚠️ **Eles invalidaram um numero que eles mesmos tinham oferecido:** o par
`recebido_em`/`processado_em` nao se compara — o primeiro tem granularidade de SEGUNDO, o segundo tem
microssegundos, e a diferenca mede distancia da borda do segundo, nao latencia. **Sai das duas
medicoes.**
🔴 **O que quase custou isso:** a medicao foi pedida as 23:52 **no arquivo errado** (o deles), e
reenviada as 00:02 no certo. Depois eu escrevi as 00:49 **sem reler** e cobrei o que ja estava
entregue as 00:15 e as 00:28 — a resposta deles ficou **cinco horas** parada. As duas licoes estao em
`github/docs/CANAL-ENTRE-SESSOES.md`: *o seu arquivo e' o que mora no repositorio do OUTRO*, e
*"eu li" tem prazo de validade — releia no movimento de ESCREVER*.

✅ **FECHADO: o passo 1 da T-189 esta NO AR desde 2026-08-31 00:15** (BACKEND 3.236.0, 6.235 testes
verdes, 15 guardas novas). **A T-189 nao esta mais bloqueada.**
🔴 **E eles contradisseram a forma que a gente pediu, com razao medida:** em vez de `novo or velho`
em **55** leitores espalhados por 13 arquivos, traduzem **uma vez na porta** (10 pontos). Cinquenta e
cinco pontos de edicao sao cinquenta e cinco chances de esquecer um, e **o esquecido nao falha** —
`.get()` ausente vira `None`, vira string vazia, e a mensagem sai errada sem acordar ninguem.
*E' o mesmo argumento que usamos para inverter o portao de telefone na T-191: enumeracao esquece o
item novo, e o esquecido e' invisivel.* **A contradicao foi o produto do canal, nao o atrito.**

📌 **O canal sao DOIS arquivos, e confundi-los ja custou 32 minutos de silencio invisivel:**

| arquivo | quem ESCREVE | quem LE |
|---|---|---|
| `C:\dev\<consumidor-b>\zapgw-STATUS.local.md` | **nos** | eles |
| `C:\dev\zapgw\<consumidor-b>-STATUS.local.md` | **eles** | nos |

🔴 **`<consumidor-b>` e' pseudonimo de proposito: este repositorio e' PUBLICO e nome de cliente nao
entra.** Para resolver o nome na maquina, `ls *-STATUS.local.md` na raiz — o arquivo esta la, e esta
gitignorado. *Nao escreva o nome real aqui para "facilitar": e' irreversivel.*

O caminho antigo (`C:\dev\zapgw-dev`) esta morto nos dois sentidos, e as 7.418 linhas de historico ja
foram copiadas. **A seçao orfa das 23:52 fica no topo do arquivo deles** — nao se apaga arquivo do
outro, nem para desfazer bobagem propria.
🔴 **`*.local.md` esta no `.gitignore` desde 2026-08-30 e tem de continuar** — o canal carrega
telefone real e `wamid` de producao, e este repositorio e' publico.


🙋 **DUAS COISAS QUE SO' O DONO DECIDE, e nenhuma corre.** A migracao do contrato acabou (T-189,
`v0.63.0` em producao e provada pelo consumidor). Sobram estas, ambas com raio de alcance grande:

1. **Apagar o apelido de ENTRADA.** Hoje um pedido em portugues continua funcionando, e e' essa rede
   que segura o que ninguem previu. **A autorizacao de 31/08 foi para a VIRADA, nao para apagar a
   rede** — isto e' outra conversa. Quando for a hora, o portao e o mesmo: `nome_antigo_usado` em
   zero **e** volume subindo ao lado, agora com as treze chaves que ele passou a enxergar.
2. **Os pares dos 18 nomes de contador.** Eles continuam em portugues **de proposito** — o doc que
   dizia que a tabela ja os carregava era falso, e a correcao esta na secao 8.11.
   🔴 **O consumidor alarma em 8 dos 18**, entao renomear sem ele no circuito quebra alarme em
   silencio. Decidir os pares passa por ele antes.

📌 **Uma lacuna que o consumidor declarou e que so' o tempo fecha:** o valor `mensagem` -> `message`
do tipo de evento so' aparece quando **uma cliente escrever** — nao ha como fabricar. O mecanismo ja
esta provado num valor que muda (`observed` -> `observado`); falta a combinacao especifica. Se der
errado, o sintoma e' evento **preso com aviso**, nao evento sumido — por causa do conserto que eles
fizeram no `processado_em` hoje de manha.

## Active

> A fila do periodo privado esta em `iscarelli/zapgw-dev`, congelada. Tarefa nova nasce aqui.
## [ ] T-212  CAMADA 1: file names and identifiers stop speaking Portuguese
After:  T-211
Why:    **Decisao do dono, 2026-08-31: limpar o sistema de tudo em PT-BR.** Esta e' a camada que **nao
        toca contrato nenhum** e que o compilador confere: **69 arquivos `.go`** com nome em portugues
        e **38 identificadores** medidos. E' a maior em volume e a de menor risco — por isso vem
        primeiro.
Files:  os 69 arquivos e quem os referencia

Do:
  1. **Renomeie arquivo e identificador para ingles.** `git mv` para os arquivos, para o historico
     seguir o arquivo.
  2. 🔴 **NAO toque em tag `json`, valor emitido, nome de variavel de ambiente, verbo de CLI, nem
     string de mensagem.** Esta camada e' **interna**. Se voce se pegar editando uma dessas, parou:
     sao as camadas 2, 3 e 4, e cada uma tem risco proprio.
  3. **Nome de teste tambem** — eles descrevem comportamento e sao lidos por quem investiga.
  4. Faca em lotes, com o verify entre eles.

Verify:
  - `CGO_ENABLED=0 go build ./...` e `go test -count=1 ./...` verdes — o compilador e' o portao desta
    camada.
  - 🔴 **`git diff --stat` nao pode conter mudanca em tag `json`.** Confira com
    `git diff -U0 | grep 'json:"'` — tem de sair vazio.
  - **A varredura do contrato (`TestOutputContractHasNoPortugueseKeyOrValue`) continua verde sem
    edicao.**
  - Diga quantos arquivos e quantos identificadores mudaram.

## [ ] T-213  CAMADA 3, primeira metade: measure which Portuguese strings REACH the consumer
After:  T-212
Why:    Ha **207 strings de producao em portugues** medidas. Parte e' log interno — grátis de trocar.
        Parte viaja no corpo da resposta, dentro de `erro`, `motivo`, `explicacao_meta`, e **o
        consumidor pode estar comparando com ela**.
🔴      **Ninguem sabe hoje qual e' qual**, e trocar as 207 sem separar e' mudar contrato as cegas.
Files:  docs/INVENTARIO-STRINGS.md (novo)

Do:
  🔴 **NAO traduza nada nesta tarefa. Ela MEDE.**
  - Para cada string de producao em portugues: `arquivo:linha`, e a **classificacao**:
    **SAIDA-CONSUMIDOR** (chega no corpo de uma resposta ou evento), **LOG** (só stderr/arquivo), ou
    **AMBOS**.
  - **Meça, nao deduza:** siga o caminho da string ate um `w.Write`/`json.Encode` ou ate um `log.`.
    Se nao conseguir decidir, escreva `A MEDIR` e diga o que faltou.
  - Diga quantas sao de cada tipo. **O numero de SAIDA-CONSUMIDOR e' o tamanho real do problema.**

Verify:
  - Amostra de 6 ponteiros conferida com `sed -n`.
  - Total bate com as 207 (ou a diferenca explicada).
  - Verify de sempre limpo.

## [ ] T-214  CAMADA 4: `ZAPGW_*` and the CLI accept both names, and count the old one
After:  T-213
Why:    **24 variaveis `ZAPGW_*`** e os verbos de CLI (`consumidor`, `estado`, `fumaca`, `instancia`).
🔴      **O risco nao e' o rename — e' que ele NAO ALCANCA o `/etc/zapgw/env`.** O gateway sobe com o
        default, **em silencio**: nao quebra, nao avisa, e ninguem descobre ate alguma coisa que
        dependia da variavel nao acontecer. O `CLAUDE.md` ja marca isto como fora de escopo da T-189
        por esse motivo.
        **O "consumidor" desta camada e' o OPERADOR**, e o `/etc/zapgw/env` e' o escritor que precisa
        migrar. Entao vale o mesmo jogo de quatro passos que funcionou com o contrato.
Files:  cmd/zapgw/, internal/config/

Do:
  1. **Aceitar os dois nomes** — variavel e verbo —, com o novo tendo precedencia se ambos vierem.
  2. **Contar o uso do nome velho**, visivel de fora (no `/v1/estado` ou no log de arranque), porque
     e' esse numero que autoriza remover depois.
  3. 🔴 **Se o nome velho for usado, DIGA no arranque** — uma linha por variavel, uma vez. O operador
     precisa ver, e o arranque e' o unico lugar onde ele olha.
  4. 🔴 **NAO remova nenhum nome velho nesta tarefa.** A remocao e' outra conversa e e' do dono.

Verify:
  - Para cada variavel e cada verbo: nome velho funciona **e conta**; nome novo funciona e nao conta.
  - **Ambos presentes: o novo vence, e isso tem teste.**
  - **Arranque com nome velho imprime o aviso**; sem nome velho, nao imprime nada.
  - Verify de sempre limpo.

