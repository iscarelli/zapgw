# Migrar um consumidor para o zapgw

*[Read in English](MIGRACAO-PARA-O-ZAPGW.md)*

**Código:** [`CONTRATO-CONSUMIDOR.md`](CONTRATO-CONSUMIDOR.md) (o que o consumidor precisa cumprir) ·
o runbook de armar a entrada (fica no repositorio privado — e operacao da casa, nao produto) ·
the deployment runbook (kept in the private repository) (onde o gateway roda) · `internal/inbound/deliver.go` ·
`internal/outbound/handler.go`

> **Escopo.** Este documento descreve o que o zapgw **exige e garante**, e em que ordem a troca tem de
> acontecer. Ele **não** descreve mudanças no código do consumidor: cada repositório é lido por quem
> trabalha nele. Aqui é o contrato e a sequência.

---

## Duas migrações muito diferentes usam este documento

| | Consumidor **em teste** | Consumidor **em produção** |
|---|---|---|
| Janela de parada | não precisa | **não pode haver** |
| Convivência dos dois caminhos | dispensável | **obrigatória** |
| Ensaio de rollback | dispensável | **obrigatório** |
| Ordem dos passos | conselho | **estrutura** |
| Dedup do consumidor | "deve ter" | **pré-requisito bloqueante** |

O primeiro consumidor (`consumer-a`, 2026-07-26) estava **em teste**, e foi isso que tornou a
migração dele barata — coube num dia porque errar era reversível. **Esse desconto não se repete.**
Se o seu sistema atende gente agora, leia a coluna da direita e trate cada linha dela como requisito,
não como zelo.

---

## O que a migração É, e o que ela NÃO é

**É trocar quem fala com a Meta.** O consumidor deixa de chamar a Cloud API (direto ou através de uma
camada como o Evolution) e passa a chamar o gateway, que chama a Meta.

**NÃO é entrar no ecossistema oficial.** Se o seu número hoje está numa conexão **não-oficial**
(WhatsApp Web, Baileys e afins), isto aqui não serve: você precisa antes registrar o número na Cloud
API, submeter templates para aprovação (dias), e aceitar a janela de 24 h e o custo por conversa —
**e o número não pode estar ativo nos dois mundos ao mesmo tempo**, o que elimina a convivência.
Nesse caso, "não pode parar" vira o requisito mais difícil do projeto.

> **Confirme isto antes de tudo.** Payload com forma de Cloud API não prova conexão oficial: camadas
> intermediárias normalizam formato. Pergunte a quem administra o número, ou olhe no Business Manager
> se o número aparece como registrado na Cloud API.

---

## Fase 0 — os portões. Nada é marcado antes disto.

**Nenhuma data de virada se marca enquanto qualquer item aqui estiver aberto.**

### 0.1 — O gateway já atendeu duas instâncias? (lado do gateway) — **SIM, em laboratório**

Se você é o **segundo** consumidor, a sua migração é a primeira vez que o gateway atende dois
consumidores **de verdade** ao mesmo tempo. O que já foi feito, e o que continua em aberto, é a
distinção que importa aqui.

Com **um App por consumidor** (o padrão desta casa), o isolamento de entrada é o **caminho da URL**
mais o `app_secret` **daquela** instância: cada instância recebe em `/v1/inbound/{slug}`, e
`app_secret`/`verify_token` são de Apps diferentes.

**Exercitado com tráfego em 2026-07-26 (T-042)**, no binário de produção do CT 125, com uma segunda
instância de teste criada e removida no mesmo dia:

- POST assinado no caminho da instância de teste → `200`, e o envelope chegou **só** ao coletor de
  teste. Os contadores da `tenant-one` não se moveram (24/24 antes e depois);
- **os mesmos bytes, com a mesma assinatura válida, no caminho da outra instância → `403`
  `assinatura invalida`.** É esta metade que prova que o segredo é por instância; a primeira só
  provaria roteamento;
- o mesmo com o `verify_token` no `GET` de verificação: `200` + desafio no caminho dele, `403` no
  caminho da outra;
- o alarme de `phone_number_id` alheio — que **nunca havia disparado na vida do serviço** — foi
  provocado de propósito e **disparou**.

**O que isso NÃO prova, e não pode ser lido como se provasse: a Meta entregando de fato no caminho da
segunda instância.** A origem do tráfego acima fomos nós, assinando com um segredo que nós mesmos
escolhemos — é o mesmo binário e o mesmo código, não a mesma origem. Essa prova é de rede real e mora
no portão 0.4 (`zapgw fumaca` da instância de verdade), não aqui.

### 0.2 — A sua deduplicação é ATÔMICA? (lado do consumidor — **bloqueante**)

```python
if Evento.objects.filter(evento_id=eid).exists():   # ← NÃO é dedup
    return
```

Se a sua dedup é assim, **pare aqui**. Ela falha sob concorrência, e concorrência não é hipótese: a
Meta reentrega **5 vezes em 9 segundos** quando não recebe `200` a tempo. Duas entregas do mesmo
evento passam as duas pelo `exists()` e as duas inserem.

E o custo não é uma linha repetida — **os efeitos colaterais rodam de novo**: mensagem duplicada na
tela, reencaminhamento duplicado, **resposta automática enviada duas vezes para a mesma pessoa**, e
cota da Meta queimada.

O que resolve: restrição no banco, e violação tratada como **sucesso**.

```sql
CREATE UNIQUE INDEX CONCURRENTLY uniq_evento_id ON eventos (evento_id) WHERE evento_id <> '';
```

Detalhes e o porquê em [`CONTRATO-CONSUMIDOR.md`](CONTRATO-CONSUMIDOR.md), seção *Deduplique POR
EVENTO*.

### 0.3 — Separe o código VIVO do resto de integração abandonada

Projeto que já tentou outro caminho antes deixa artefatos que **leem como atuais**: endpoints,
parsers, clientes, configuração. **Conclusão tirada de código morto parece fundamentada** — tem
`arquivo:linha` — e não é.

Aconteceu em 2026-07-26: um consumidor concluiu *"meu rollback pode não existir"* lendo um endpoint
que era resto de um estudo interrompido, não o caminho vivo.

**Prove com tráfego, não com leitura** — e o teste que separa vivo de morto é **dado que só o
caminho vivo produz**: linha no banco, contador, efeito observável. Não precisa ser log novo. O
consumidor acima fechou a questão com uma consulta de leitura em produção — *795 mensagens
recebidas, a última três horas atrás, e todas criadas exclusivamente por aquele endpoint* —, e um
`print` temporário teria custado um deploy para provar o que o banco já dizia. **Log novo é o
recurso de quando o caminho não deixa rastro**; se ele deixa, o rastro já é a prova.

O mesmo levantamento achou o resíduo que a leitura não acha: **três tarefas periódicas do estudo
abandonado rodando no worker, a cada 60 s e 300 s, contra tabelas vazias.** Não interceptavam nada
— e por isso ninguém teria olhado. Enumere o resíduo mesmo depois de a suspeita cair.

### 0.4 — A instância existe, está pausada, e o teste de fumaça passou

Instância nasce **pausada** (`ativo=0`) de propósito. Só `zapgw fumaca` a ativa, e ele manda uma
mensagem de verdade — o que prova token, número e conectividade **antes** de qualquer consumidor
depender disso.

### 0.5 — O endpoint de recebimento do consumidor está no ar e provado

Não basta responder `200`. Ele precisa cumprir as obrigações do contrato — e a que mais custa é:
**responder `200` só depois de ter gravado.** O `200` do consumidor faz o gateway dizer à Meta que
está resolvido, e **ela nunca mais reenvia**.

Prove com uma requisição sem assinatura: tem de dar `5xx` (não `403` — ver a seção de erro de
credencial no contrato).

---

## Fase 1 — ENVIO. Reversível a qualquer instante.

O consumidor passa a chamar `POST /v1/messages` em vez da Meta (ou da camada antiga).

**Por que esta fase vem primeiro:** ela é **inteiramente reversível e do lado do consumidor**. Não há
mudança de configuração na Meta, não há passo sem volta. Se algo der errado, o interruptor é uma
variável de ambiente.

Recomendado: **um interruptor que, vazio, mantém o caminho antigo**. Assim o código novo vai a
produção inerte, e a virada é uma linha de `.env` — com alguém olhando.

Comece pelo tipo mais barato de errar: um aviso de teste, para o número de quem está olhando. **Se
falhar, falha para uma pessoa que está vendo, não para um cliente.**

Três coisas que mordem na primeira integração:

1. **A porta `8443` não é detalhe.** O envio só existe no entrypoint interno. Na `:443` a mesma URL
   devolve `404`, **de propósito**.
2. **Não desabilite a verificação de TLS.** O que trafega ali é o seu token.
3. **`Idempotency-Key` identifica a INTENÇÃO, não a tentativa.** O id da sua linha de fila serve; um
   UUID novo a cada retry não serve para nada. E **o corpo tem de ser byte a byte estável entre
   tentativas da mesma chave** — se qualquer parte do texto for computada do relógio ("vence
   amanhã"), o retry vira `422`.
   **Congele o texto no PRIMEIRO DESPACHO, persista o valor congelado, e só então chame o
   `POST /v1/messages` — nunca recompute se já houver valor persistido.** Nesta ordem, e as três
   partes têm motivo:
   - **no primeiro despacho, não no nascimento da linha da fila** — esta doc dizia "no nascimento" e
     estava errada. Um consumidor derrubou a recomendação com o caso dele: o sistema tem **janela de
     envio** (fora de 8h–20h a mensagem é adiada para a janela seguinte, e isso é o caminho normal,
     não a borda). Congelado no nascimento, um disparo das 21:00 chega às 08:00 dizendo *"Boa
     noite"*. O `422` é risco **condicional** — só morde se houver retry cruzando a fronteira;
     a saudação errada é erro **certo**, visível para o cliente, em toda mensagem adiada;
   - **persista** — senão uma reconstrução (worker reiniciado, linha reenfileirada) recomputa e o
     `422` volta pela porta dos fundos;
   - **persista ANTES da chamada HTTP** — é a mesma obrigação da inbound (*grave o cru antes de
     responder*). Persistir depois deixa a janela em que o gateway já tem `chave → corpo` e você não
     tem o corpo: o retry recomputa e leva `422`. É exatamente o buraco que congelar existe para
     fechar.

   > **O caso que ninguém audita é a saudação, não a data.** Um consumidor foi procurar se este
   > aviso o atingia e achou `"Bom dia"/"Boa tarde"/"Boa noite"` — derivado de `now().hour` —
   > preenchendo uma variável de **quase todos** os templates dele. O corpo muda sozinho às 12:00 e
   > às 18:00: tentativa às 11:59 + retry às 12:01 = mesma chave, corpo diferente = `422` permanente,
   > e a mensagem não sai. "Vence amanhã" ao menos **parece** conteúdo transacional e é conferido;
   > saudação parece decoração. E o timing conspira: retry acontece quando algo está lento — que é
   > quando a chance de cruzar a fronteira é maior. Procure **tudo** que lê o relógio, não só datas.

---

## Fase 2 — RECEBIMENTO. O único passo sem volta imediata.

```
1. cadastrar a callback_url na instância        ← zapgw instancia rotacionar --callback-url
2. rotacionar o app_secret real no gateway      ← precisa do valor do SEU App
3. apontar o Callback URL do SEU App para cá    ← IRREVERSÍVEL no instante em que salva
4. mensagem de teste do celular, ciclo completo ← a prova
```

**Os passos 1 e 2 vêm antes do 3, sempre.** Ao contrário, a Meta começa a entregar num gateway que
ainda não sabe conferir a assinatura: toda entrega é recusada, ela reenfileira por 36 h e o alarme
dispara à toa. Não há perda — há ruído e uma investigação inútil.

**Não existe convivência no recebimento.** O webhook é **por App**: no segundo em que a URL é salva,
aquele número entrega aqui e em nenhum outro lugar. Isso não é limitação do gateway; é da Meta.

### Voltar atrás

**Reapontar o Callback URL do seu App para o endpoint anterior.** É a única coisa a desfazer.

Para que isso funcione, **o endpoint antigo tem de continuar no ar** — não o desligue no mesmo
movimento. Mantenha-o vivo até o ciclo estar provado e você ter dormido uma noite.

**Ensaie antes.** Saiba de antemão quantos cliques e quanto tempo leva reapontar, e quem tem acesso
para fazê-lo. Rollback que ninguém ensaiou é rollback que demora justamente no dia em que a pressa
importa.

> **Escreva a URL exata do endpoint anterior ANTES da virada**, junto com o App ID, num lugar que
> você acha em dez segundos. Formulação de um consumidor que preparou a própria migração:
> *"rollback cuja primeira etapa é **achar o valor que eu preciso digitar de volta** é rollback
> lento, e a lentidão chega no pior dia."*
> Você vai substituir esse valor no painel — e o painel não guarda o anterior.

**Descubra para onde a Meta entrega hoje sem depender do painel.** Se o seu endpoint atual exige algo
que a Meta não manda — um `Authorization: Bearer`, por exemplo —, então a Meta **não** entrega nele
direto: entrega na camada intermediária, que repassa. Um consumidor provou exatamente assim, por
`arquivo:linha`, sem acesso ao painel: *"se o Callback URL apontasse para o meu Django, toda entrega
levaria `401`, e o sistema funciona há meses"*. Restrição viva prova topologia melhor que leitura de
configuração.

Para parar o recebimento **sem** mexer na Meta: pausar a instância. Ela passa a responder `503`, a
Meta reenvia por 36 h, e uma pausa curta **não perde mensagem**. Uma pausa longa perde, em definitivo
e em silêncio.

---

## O que o consumidor ganha, e não é óbvio

- **`GET /v1/instances/{slug}/health`** acusa token revogado na Meta — a falha que morre calada e
  cuja primeira notícia costuma ser o cliente não receber.
- **`GET /v1/templates`** devolve o catálogo **inteiro**, sem truncar.
- **`POST /v1/media`** dispensa hospedar URL pública para a Meta buscar.
- **`cobranca` no evento de status** diz sob qual categoria a Meta cobrou — a reclassificação
  `UTILITY` → `MARKETING` aparece na primeira mensagem, não na fatura.
- **Os dois mimes da mídia chegam separados.** Quem reenvia áudio precisa do `mime_do_payload` — com
  o outro, o WhatsApp entrega **anexo** em vez de nota de voz, sem erro nenhum. Custou caro nesta
  rede em 2026-07-20.

## O que este documento NÃO garante

- **Que o seu caminho atual é Cloud API oficial.** Confirme (ver acima). Se não for, este documento
  é o errado.
- **Que a sua camada atual só passava mensagens.** Se o Evolution (ou equivalente) também guardava
  mídia, gerenciava sessão ou fazia qualquer coisa além de repassar, isso **não vem junto** — levante
  o que ele faz hoje antes de tirá-lo do caminho.
- **Que outro sistema não depende do mesmo número.** Se depender, a Fase 2 afeta esse sistema no
  mesmo instante, e ninguém verificou.
