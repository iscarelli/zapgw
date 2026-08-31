# Manual do integrador — zapgw

*[Read in English](MANUAL-DO-INTEGRADOR.md)*

**Código:** `internal/outbound/cadastro_handler.go`, `internal/outbound/fumaca.go`,
`internal/outbound/fumaca_handler.go`, `internal/outbound/pausa_handler.go`,
`internal/inbound/handler.go`, `cmd/zapgw/provisionar.go`, `internal/config/store.go`,
`internal/meta/instagram.go`, `internal/outbound/renovador_instagram.go`.

*(Esse bloco é metadado de manutenção do gateway — ele responde "que doc esta mudança de código
quebrou?". **Você não precisa dele**, e nada abaixo depende de você ter esses arquivos.)*

---

Este é o manual de **implantação**: *o que você faz, em que ordem, para sair do zero e ficar
operando*. Ele é curto de propósito.

**Ele não descreve o comportamento das rotas.** Isso está em
[`CONTRATO-CONSUMIDOR.md`](CONTRATO-CONSUMIDOR.md) — corpos, campos, erros, garantias e limites.
São dois documentos porque são duas perguntas diferentes, e **duas cópias da mesma coisa divergem**.
Aqui a sequência; lá o comportamento. Quando este manual precisar de um detalhe de rota, ele aponta
para lá em vez de repetir.

**Para quem ele é:** um programador com **conta Meta própria**, que recebeu um slug de quem opera o
gateway e **não tem canal para perguntar nada**. Se você está lendo isto, presuma que ninguém vai
responder e-mail: o que você precisa saber está escrito aqui ou no contrato.

🔴 **E ele cobre WhatsApp. Se o seu slug for de INSTAGRAM, pare aqui e leia o parágrafo abaixo.**
Uma instância de Instagram existe (desde 2026-07-30, em produção), mas a **sequência é outra** e
boa parte dos passos deste manual **não se aplica a ela**:

- **não há `POST /v1/cadastro` para Instagram**, e portanto não há janela de 24 h. A identificação da
  conta (`ig_id`) só entra **na criação da instância**, feita por quem opera o gateway — logo os
  Passos 2, 3 e 4 deste manual, que são sobre você preparar e cadastrar a sua conta, **são executados
  por essa pessoa**, não por você;
- o `ig_id` que vale é o **`entry[].id` que chega no webhook** — **não** o que `GET /me` devolve, que
  é outro espaço de id. *Confundir os dois já fez este gateway descartar tráfego real;*
- o envio é só **texto** nesta primeira fatia, e o token tem validade de 60 dias, renovada
  automaticamente pelo gateway.

**O que vale para você, se o seu slug é de Instagram:** o comportamento das rotas está na seção
*Instagram — a primeira fatia* do [`CONTRATO-CONSUMIDOR.md`](CONTRATO-CONSUMIDOR.md), e o Passo 5
(provar o canal com o fumaça) e o Passo 6 (operar) deste manual continuam valendo. **Os Passos 2, 3 e
4, não.**

*Isto está dito aqui, e não omitido, porque um manual que descreve só um dos dois caminhos manda
quem está no outro seguir uma sequência que vai falhar — e a pessoa não tem a quem perguntar.*

---

## Índice

- [🔴 Antes de tudo: o cadastro NÃO se alcança de fora, e isso é DECISÃO](#-antes-de-tudo-o-cadastro-não-se-alcança-de-fora-e-isso-é-decisão)
- [O mapa, numa tela](#o-mapa-numa-tela)
- [Passo 1 — Confira o que você recebeu](#passo-1--confira-o-que-você-recebeu)
- [Passo 2 — Prepare a SUA conta Meta](#passo-2--prepare-a-sua-conta-meta)
- [Passo 3 — Cadastre a sua Meta no gateway](#passo-3--cadastre-a-sua-meta-no-gateway)
  - [🔴 A janela de 24 h — o erro mais caro deste manual](#-a-janela-de-24-h--o-erro-mais-caro-deste-manual)
- [Passo 4 — Aponte o webhook no painel da SUA Meta](#passo-4--aponte-o-webhook-no-painel-da-sua-meta)
- [Passo 5 — Prove o canal (e só então a instância ativa)](#passo-5--prove-o-canal-e-só-então-a-instância-ativa)
- [Passo 6 — Operar](#passo-6--operar)
- [Quando algo dá errado, olhe nesta ordem](#quando-algo-dá-errado-olhe-nesta-ordem)
- [As três coisas que só a outra pessoa resolve](#as-três-coisas-que-só-a-outra-pessoa-resolve)

---

## 🔴 Antes de tudo: o cadastro NÃO se alcança de fora, e isso é DECISÃO

**Leia isto antes de reservar tempo de equipe.** De todas as rotas do gateway, **apenas o webhook**
(`POST /v1/inbound/{slug}`, que quem chama é a **Meta**) está publicado na internet. Todo o resto —
**inclusive o `POST /v1/cadastro`** — responde só na rede interna do gateway.

**Isso não é obra inacabada nem lacuna esperando conserto: é escolha do dono do gateway, e fica
assim até haver necessidade que a justifique.** Não planeje em cima de uma data, porque não existe
uma.

Consequência direta, e é ela que você precisa acomodar: **o Passo 3 deste manual — onde entram o
`app_secret` e o `token_envio` da sua conta Meta — é executado por quem alcança a rede do gateway.**
Na prática, você entrega os seus valores a essa pessoa e ela cadastra por você. Todo o resto do
fluxo continua sendo seu.

---

## O mapa, numa tela

| # | Quem faz | O quê | Depende de |
|---|---|---|---|
| 1 | **quem opera o gateway** | cria a sua instância e te entrega **5 valores** | nada |
| 2 | **você** | prepara a **sua** conta Meta (App, número, token permanente) | nada nosso |
| 3 | **você** | `POST /v1/cadastro` — grava a sua Meta no gateway | 1 e 2 |
| 4 | **você** | aponta o webhook no painel da **sua** Meta | 1 |
| 5 | **você** | `POST /v1/fumaca` — prova o canal. **Só isso ativa a instância** | 3 |
| 6 | **você** | opera: envia, recebe, lê estado | 5 |

Os passos 2 e 4 acontecem no painel da Meta, na **sua** conta. Os passos 3, 5 e 6 são chamadas HTTP
ao gateway. **O passo 1 acontece uma vez e não se repete.**

⏱ **Quanto tempo leva:** os passos 3 a 5 levam minutos. O passo 2 leva de horas a dias, e é ele que
manda no calendário — a parte lenta é a Meta, não nós.

---

## Passo 1 — Confira o que você recebeu

A instância é criada **manualmente** por quem opera o gateway, e é ele quem escolhe o **slug** (ele é
imutável e vira caminho de URL). Ele **não conhece, não guarda e não pede** os dados da sua conta
Meta.

Você recebe **cinco itens**, numa conversa que acontece uma vez:

| # | O quê | Para que serve |
|---|---|---|
| 1 | o **slug** | identifica a instância em **todo** corpo de requisição (`"instancia": "<slug>"`) |
| 2 | a URL de **cadastro** | a base das rotas que **você** chama |
| 3 | a URL de **webhook** (`…/v1/inbound/<slug>`) | você cola em *Callback URL* no painel da sua Meta |
| 4 | `verify_token` **e** `segredo_entrega` | o primeiro você digita em *Verify Token* na Meta; o segundo confere a assinatura de cada entrega que o gateway te faz |
| 5 | o **token de consumidor** | é o `Authorization: Bearer` de toda chamada sua |

🔴 **Confira os cinco AGORA, antes de começar.** Os dois valores do item 4 são sorteados pelo gateway
e **mostrados uma única vez** — ele guarda só o cifrado e não os mostra de novo. Item faltando vira
trabalho parado do seu lado, e a única saída é voltar a quem te entregou o slug.

**Não falta mais nada.** Não existe um sexto valor que alguém esqueceu de mandar. Tudo o que não está
nessa lista ou é **seu** (você mesmo cadastra, no Passo 3) ou está escrito no contrato.

> 🔴 **O token de consumidor (item 5) é credencial de administração, não só de envio.** Quem o roubar
> **reconfigura a sua instância** e aponta o seu tráfego para a Meta dele — enquanto a janela de 24 h
> do Passo 3 estiver aberta. Não o deixe em repositório, em log nem em transcript de terminal.
> Detalhes e o que cada segredo vale na mão de outra pessoa: contrato, *"O que cada segredo permite a
> quem o roubar"*.

---

## Passo 2 — Prepare a SUA conta Meta

Tudo aqui acontece na **sua** conta, e nada disso depende do gateway. Ao fim você tem **quatro
valores**, que são o que o Passo 3 pede:

| Valor | O que é | Onde |
|---|---|---|
| `waba_id` | id da WhatsApp Business Account | Business Settings → contas do WhatsApp |
| `phone_number_id` | id do **número** na Graph API (**não** é o telefone) | painel do WhatsApp, depois de o número estar registrado |
| `app_secret` | a chave secreta do App | App → Configurações → Básico → Chave Secreta |
| `token_envio` | token **permanente** de System User | Business Settings → Usuários do sistema |

A ordem que funciona:

1. **Conta Business + App.** Crie a Meta Business account, crie um App do tipo Business e adicione o
   produto **WhatsApp**. Rápido, e já dá acesso à Cloud API.
2. **Número.** Adicione um número ao WABA. Ele **não pode** estar ativo num WhatsApp comum ou no
   WhatsApp Business — se estiver, desvincule antes. Ele precisa receber SMS/ligação para o código.
3. **Nome de exibição.** Passa por revisão da Meta e **pode ser recusado** por política. Recusa
   recomeça a espera, então leia a política vigente antes de submeter.
4. **Método de pagamento** no WABA. A Cloud API cobra por conversa acima da faixa gratuita.
5. **Token permanente.** 🔴 **O token que o painel te dá de cara é temporário e não serve.** Em
   *Business Settings → Usuários do sistema*, crie um **System User**, dê a ele acesso ao WABA **e**
   ao App, e gere um token permanente com escopos de mensagens e de gestão do WhatsApp Business.
   Esse é o `token_envio`.
6. **App Secret.** Guarde o valor de App → Configurações → Básico. É o `app_secret`.

> ⚠️ **A Meta muda esse processo com frequência, e este manual não afirma como fato o que não
> conferiu.** As **etapas** e as **dependências** acima são estáveis e é o que importa para
> planejar. Os **números** e os **nomes de menu** — prazos, limites de tier, escopos exatos, se o seu
> caso exige App Review — confira em `developers.facebook.com/docs/whatsapp` **no dia**, não por esta
> página.

**A verificação do negócio (Business Verification) NÃO é portão para começar.** Existe um tier
não-verificado que envia desde já, com um teto de conversas iniciadas pelo negócio por 24 h. A
verificação **destrava volume**, não habilita o envio: comece-a em paralelo, mas não espere por ela
para integrar. *(O teto vigente e o que conta como "conversa" mudam — confira na fonte.)*

**Uma instância do gateway = um App da Meta.** O que separa você de outro inquilino é a **assinatura**
de cada webhook, e ela só distingue quando os `app_secret` são diferentes. Com App próprio, isso é
garantido por construção — mas não tente pendurar dois números de Apps diferentes no mesmo slug.

---

## Passo 3 — Cadastre a sua Meta no gateway

**A direção é você → gateway, e é escrita.** O gateway recebe configuração; ele nunca a devolve.

```
POST /v1/cadastro
Authorization: Bearer <token de consumidor>
Content-Type: application/json
```

```jsonc
{
  "instancia":       "<o seu slug>",
  "waba_id":         "…",
  "phone_number_id": "…",
  "numero_exibido":  "…",           // o telefone como o cliente o vê
  "app_secret":      "…",
  "token_envio":     "…",
  "callback_url":    "https://…",   // para onde entregamos o que a Meta mandar
  "bundle_ca":       ""             // opcional: PEM da SUA CA, se você não usa CA pública
}
```

Três coisas que decidem se isso dá certo na primeira vez:

- **O cadastro SUBSTITUI: mande sempre o conjunto completo.** Campo omitido vale como **vazio**, não
  como "não mexa". É deliberado — um cadastro parcial exigiria que você soubesse o que já está
  gravado, e é justamente isso que o gateway nunca devolve. **Recadastrar é o seu caminho de
  rotação:** trocou o `app_secret` na Meta, mande o conjunto de novo.
- **A `callback_url` tem de ser `https://`, e o certificado dela é verificado.** Não existe modo de
  não verificar, em caminho nenhum deste gateway. Se o seu endpoint usa **CA própria**, mande o PEM
  dela em `bundle_ca` — isso troca a âncora de confiança, não desliga a verificação.
- **`callback_url` vazia é escolha legítima**: significa "instância só de saída", e nada é entregue a
  você. Só não a deixe vazia por descuido — confira no `200`, que diz `cifrados: [{campo:
  "callback_url", cadastrado: true|false}]`.

**A resposta não devolve nada do que você mandou** — só *se* cada campo está cadastrado. Segredo
entra e não volta, e esta rota não abre exceção.

### 🔴 A janela de 24 h — o erro mais caro deste manual

Você pode escrever a configuração da sua instância por **24 h contadas da PRIMEIRA vez que você
cadastrou algo com sucesso**. Não da criação da instância (você pode demorar cinco dias para começar,
e o relógio só parte quando você escreve), e **não reiniciando a cada mudança**. Um cadastro recusado
com `400` não abre a janela — errar o corpo na primeira tentativa não custa as suas 24 h.

Depois dela, o `POST` responde `409`, **nada é gravado**, e a configuração antiga continua valendo.

> ⚠️ **Rode o Passo 5 (fumaça) LOGO APÓS o primeiro cadastro — não no dia seguinte.** O caso que mais
> bate nessa janela é `token_envio` errado ou sem permissão, descoberto **depois** de a janela ter
> fechado. Enquanto ela está aberta, o ciclo *cadastra → fumaça → corrige → recadastra* é todo seu e
> não custa nada. Depois dela, **cada correção de credencial custa uma ida a outra pessoa** — só quem
> te entregou o slug consegue reabrir a janela.

**Planeje as 24 h como um bloco de trabalho contínuo.** Cadastre quando você já tiver o número
funcionando e alguém disponível para receber a mensagem de teste.

Erros desta rota (e de todas as outras): contrato, seção da rota correspondente.

---

## Passo 4 — Aponte o webhook no painel da SUA Meta

Na configuração do WhatsApp do seu App:

- **Callback URL** = o item 3 do seu pacote (`https://…/v1/inbound/<slug>`);
- **Verify Token** = o `verify_token` do item 4.

A Meta faz um `GET` de desafio na hora de salvar; o gateway responde a ele sozinho. **Esse desafio
funciona mesmo com a instância ainda pausada** — você pode fazer este passo antes ou depois do
Passo 5, e mensagens que chegarem enquanto ela estiver pausada recebem `503`, o que faz a Meta
**reenviar** por até 36 h em vez de descartar.

Dois detalhes que pegam quem faz isso pela primeira vez:

- 🔴 **Salvar a Callback URL inscreve um CONJUNTO de campos de uma vez** — não é você quem escolhe um
  a um. O seu trabalho aqui é **revisar o que ficou inscrito**, não assinar um campo. A única forma
  de saber o que está inscrito de verdade é perguntar à Graph API pelas *subscriptions* do seu App;
  o painel não mostra isso de forma óbvia.
- **Se o seu App tem mais de um número**, configure o **override de webhook por número/WABA**. Sem
  isso, um App com vários números entrega tudo num endpoint só, e o gateway **descarta o lote que não
  bate** com o `phone_number_id`/`waba_id` cadastrados — respondendo `200`, para a Meta não ficar
  reenviando, e movendo um contador que você lê em `GET /v1/estado`. *Essas guardas existem para
  pegar erro de configuração seu; o que separa você de outro inquilino é a **assinatura**, não elas
  (contrato, "O que separa você do outro inquilino").*

Campo inscrito que o gateway não modela **não é erro**: ele chega, passa as guardas e é entregue a
você sem evento nenhum (`"eventos": []`), virando linha crua no seu banco.

---

## Passo 5 — Prove o canal (e só então a instância ativa)

```
POST /v1/fumaca
Authorization: Bearer <token de consumidor>
Content-Type: application/json
```

```jsonc
{ "instancia": "<o seu slug>", "destino": "<número que vai RECEBER, em E.164>" }
```

**O que essa rota faz, nesta ordem:** confere que a instância existe → confere que a Graph API aceita
o seu `token_envio` → **manda uma mensagem de verdade** → e só então ativa. Ela aborta no primeiro
passo que falhar, e falhar em qualquer um deixa a instância **pausada**.

🔴 **`ativo` é sempre consequência de a Meta ter aceitado o envio, nunca de você ter chamado a rota.**
Não existe flag de força, nem aqui nem em lugar nenhum. Se cadastrar ativasse, uma credencial errada
viraria uma instância "ativa" que recusa tudo — e você descobriria no primeiro cliente de verdade.

Três coisas práticas:

- **`destino` não tem default, de propósito.** Uma mensagem real vai sair para esse número, e
  mensagem enviada não se desfaz. O texto se identifica de própria boca ("teste de fumaça… não é
  preciso responder"), porque quem recebe é uma pessoa.
- ⚠️ **Escolha um destino que tenha mandado mensagem para o seu número há pouco.** O fumaça manda
  **texto livre**, e a Meta restringe texto livre a quem falou com você recentemente — fora disso,
  ela exige template. *(Regra dela, não nossa, e não medida por este projeto: confira na fonte se
  quiser o prazo exato.)* Na prática: mande um "oi" do celular de teste para o seu número comercial
  **antes** de rodar o fumaça.
- **Chamar de novo numa instância já ativa é seguro e barato:** ela **não** manda uma segunda
  mensagem, e responde `ja_estava_ativa: true`. Para forçar prova nova (depois de trocar o
  `token_envio`, por exemplo), **pause primeiro** com `POST /v1/pausa` — a volta exige fumaça novo.

**Deu `502`?** A Meta recusou. A mensagem de erro diz por quê, com a mesma classificação do envio
normal (contrato, *"O que cada erro quer dizer"*). A instância continua pausada, e **enquanto a
janela de 24 h estiver aberta você corrige e recadastra sozinho**.

---

## Passo 6 — Operar

Daqui em diante o contrato é o documento. O que existe:

| O que você quer | Rota |
|---|---|
| enviar mensagem (texto, template, mídia, reação, localização…) | `POST /v1/messages` |
| marcar uma mensagem recebida como lida | `POST /v1/leituras` |
| ler os números da sua instância (contadores, série diária, saúde do token, validade do seu certificado, qualidade do número) | `GET /v1/estado` |
| saber se o canal ainda está apto a enviar | `GET /v1/instances/{slug}/health` |
| listar e criar templates | `GET` / `POST /v1/templates` |
| subir e baixar mídia | `POST /v1/media`, `GET /v1/media/{id}` |
| tirar a instância do ar sem apagar nada | `POST /v1/pausa` |

**E cinco obrigações que são suas, não nossas.** Elas estão detalhadas no contrato, em *"As cinco
obrigações do consumidor"*, e ignorar qualquer uma delas produz defeito que só aparece em produção:

1. **Grave o `cru` ANTES de olhar os `eventos`**, e responda depois.
2. **Deduplique por EVENTO**, pelo campo `id` de dentro do corpo — e de forma **atômica**.
3. **Verifique a assinatura `X-Zapgw-Signature` e o timestamp** de cada entrega. O
   `segredo_entrega` é o que separa uma entrega nossa de uma forjada.
4. **Guarde contra regressão de status** — status chegam fora de ordem.
5. **Ao reenviar mídia, use o `mime_do_payload`**, nunca o `mime_do_get`.

🔴 **Erro de credencial ou de configuração no SEU endpoint responde `5xx`, nunca `4xx`.** `4xx` diz ao
gateway "esta entrega nunca vai dar certo, não insista" — e o que era uma variável de ambiente
faltando vira mensagem perdida em definitivo.

---

## Quando algo dá errado, olhe nesta ordem

| Sintoma | O primeiro lugar para olhar |
|---|---|
| a Meta recusa a verificação do webhook | o `verify_token` que você digitou é exatamente o item 4 do pacote? |
| nada chega ao seu endpoint | a instância está **ativa**? (`GET /v1/estado`, campo `pausada`). Instância pausada responde `503` e a Meta segura o reenvio por 36 h |
| chega ao gateway mas não a você | a `callback_url` está cadastrada? (o `200` do cadastro, `cifrados`). O certificado dela é válido? (`GET /v1/estado`, `certificado_do_callback`) |
| o envio falha com erro de credencial | `GET /v1/estado`, `token_meta` — ele diz se o token ainda vale, com carimbo de quando foi conferido |
| o cadastro responde `409` | a janela de 24 h fechou. Só quem te entregou o slug reabre |
| o fumaça responde `502` | a Meta recusou. A mensagem diz por quê; se for credencial, corrija **antes** de a janela fechar |
| o envio parou de sair, sem erro óbvio | `GET /v1/estado`, `numero_na_meta` — a qualidade e a cota diária do seu número vêm de lá |

**Antes de alarmar por silêncio, olhe `pausada`.** É o engano mais comum: uma instância pausada não
tem defeito nenhum — ela está esperando um fumaça.

---

## As três coisas que só a outra pessoa resolve

Não existe canal de suporte, e este manual é o suporte. Mas há exatamente **três** assuntos que você
não consegue resolver sozinho, e todos são com **quem te entregou o slug**:

1. **reabrir a janela de cadastro** depois de ela ter fechado;
2. **rotacionar o token de consumidor** se ele vazar (o anterior deixa de valer no mesmo instante);
3. **dizer o que aconteceu com uma instância que sumiu** (as rotas passaram a responder `404`).

Ela **não** é canal de dúvida, de pedido de campo novo nem de investigação de tráfego.

**Mudança que quebra é anunciada num lugar só:** a seção *Mudanças que quebram*, no fim do contrato.
Não há aviso por outro canal e não há endpoint que declare versão de formato — releia aquela seção
antes de subir uma integração nova.
