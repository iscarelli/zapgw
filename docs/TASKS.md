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
## [ ] T-217  Half the doc pointers are dead after the rename — fix them, and build the gate that stops it recurring
Why:    🔴 **Medido em 2026-08-31, depois da T-212:** dos **113** ponteiros `arquivo.go` nos documentos
        de `docs/`, **50 apontam para arquivo que nao existe mais**. A camada 1 renomeou 86 arquivos e
        os documentos ficaram apontando para os nomes velhos.
        **A causa e' minha:** a spec da T-212 mandava corrigir comentario de codigo que citava nome
        antigo, e **nao disse "e os docs"**. O implementador fez exatamente o que foi pedido.
🔴      **E isto e' o incidente T-190 se repetindo em 24 h** — la eram 28 ponteiros prometendo arquivos
        que nao tinham migrado. *Um doc que aponta para arquivo inexistente nao falha, nao avisa, e
        so' e' descoberto por quem foi procurar e nao achou — que e' o pior momento.*
        O `CLAUDE.md` diz que o cabecalho `Código:` existe justamente para que *"qual doc a minha
        mudanca quebrou?"* seja mecanico. **Hoje ele nao e', porque nada confere.**
Files:  docs/*.md
        internal/config/  (o portao novo)

Do:
  1. **Conserte os 50**, mapeando cada nome velho para o novo. O `git log --follow` sabe o caminho de
     cada renomeacao — use isso em vez de adivinhar pelo nome parecido.
  2. 🔴 **Construa o portao, e ele e' metade da tarefa:** um teste que varre `docs/` atras de todo
     ponteiro no formato `caminho/arquivo.go` (e `arquivo.go:linha`) e **reprova nomeando o doc, a
     linha e o ponteiro** quando o arquivo nao existe.
  3. **Falha fechada:** se a varredura nao achar ponteiro nenhum, ela **reprova** dizendo que nao
     conseguiu verificar — zero ponteiros num repositorio com 113 significa que o padrao mudou e o
     teste parou de olhar, nao que esta tudo certo.
  4. **Prove por mutacao:** aponte um doc para um arquivo inexistente, confirme que o teste reprova
     **nomeando doc, linha e ponteiro**, e desfaca. Cole a saida.
  5. **Cuidado com os falsos positivos legitimos:** doc que cita um arquivo de OUTRO repositorio
     (`zapgw-dev:...`), ou um caminho de exemplo, nao e' ponteiro quebrado. Se precisar de excecao,
     🔴 **ela e' por CAMINHO COMPLETO, nunca por palavra** — foi banir a palavra `"tipo"` inteira que
     cegou a varredura do contrato hoje.

Verify:
  - `grep` conta **zero** ponteiros mortos depois do conserto — cole o numero antes e depois.
  - A saida da mutacao do item 4.
  - A saida do controle de falha fechada (varredura sem ponteiro nenhum reprova).
  - `go test -count=1 ./...`, `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `gofmt -l cmd internal`.

## [ ] T-216  The obsolete-name warning never reaches the operator — surface it on a SUCCESSFUL deploy
Why:    A T-214 fez o gateway avisar, no arranque, quando uma variavel de ambiente com nome velho foi
        usada — porque o arranque e' o unico lugar onde o operador olha, e sem esse aviso o
        `/etc/zapgw/env` de producao nunca migra e o contador nunca chega a zero.
🔴      **Medido depois de implantar a `v0.64.0`: o aviso nao chega nele.** O `implanta/deploy.sh`
        so' mostra o journal **quando o `/v1/health` FALHA** (`deploy.sh:384-385`). Num deploy
        bem-sucedido — o caso normal, e o unico em que o operador nao vai cavar — nada aparece.
        **O aviso existe, esta correto, e e' invisivel.** *E' a mesma familia do monitor cego: um
        mecanismo que responde certo para ninguem.* E a consequencia e' concreta: a migracao do
        `/etc/zapgw/env` depende de alguem descobrir sozinho que precisa acontecer.
Files:  implanta/deploy.sh

Do:
  - **No caminho de SUCESSO**, depois do `VERSAO CONFERE`, leia o journal do arranque e **mostre as
    linhas de aviso de nome obsoleto**, se houver.
  - 🔴 **Nao despeje o journal inteiro.** Filtre pelas linhas do aviso. Despejo vira ruido, e ruido
    treina a ignorar a saida do deploy — que e' onde mora a prova da versao.
  - **Se nao houver nenhuma, diga isso em uma linha** (`nenhuma variavel com nome obsoleto em uso`).
    🔴 *Silencio nao pode ser indistinguivel de "nao consegui olhar"* — se a leitura do journal
    falhar, diga que falhou, nao que estava limpo.
  - **Nao mude o comportamento do caminho de FALHA**, que ja mostra o journal inteiro de proposito.

Verify:
  - Um deploy de verdade, com o `/etc/zapgw/env` como esta hoje: cole a linha que o deploy passou a
    imprimir — seja a lista de avisos, seja o "nenhuma".
  - **Prove os dois lados:** force uma leitura de journal que falha (comando inexistente, por
    exemplo) e confirme que a saida diz **que nao conseguiu ler**, e nao "nenhuma".
  - `bash -n implanta/deploy.sh` sem erro.


(vazia)

