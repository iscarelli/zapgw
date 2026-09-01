# Contrato do consumidor

*[Read in English](CONTRATO-CONSUMIDOR.md)*

**Código:** `internal/inbound/deliver.go`, `internal/inbound/handler.go`,
`internal/inbound/mirror.go`, `internal/inbound/testdata/assinatura-entrega.json`,
`internal/meta/types.go`, `internal/meta/parse.go`, `internal/meta/client.go`,
`internal/meta/errors.go`, `internal/meta/media.go`, `internal/meta/templates.go`,
`internal/meta/read.go`, `internal/meta/number.go`, `internal/meta/instagram.go`,
`internal/meta/block.go`,
`internal/outbound/handler.go`,
`internal/outbound/message.go`, `internal/outbound/body.go`, `internal/outbound/health_handler.go`,
`internal/outbound/media_handler.go`, `internal/outbound/templates_handler.go`,
`internal/outbound/reads_handler.go`, `internal/outbound/block_handler.go`,
`internal/outbound/state_handler.go`,
`internal/outbound/state.go`, `internal/outbound/instagram_renewer.go`,
`internal/outbound/external_probe.go`,
`internal/outbound/registration_handler.go`, `internal/outbound/smoke.go`,
`internal/outbound/smoke_handler.go`, `internal/outbound/pause_handler.go`,
`internal/config/store.go`, `internal/config/counter.go`, `cmd/zapgw/provision.go`,
`cmd/zapgw/main.go`.

*(Esse bloco é metadado de manutenção do gateway — ele responde "que doc esta mudança de código
quebrou?". **Você não precisa dele**, e nada abaixo depende de você ter esses arquivos.)*

Este documento é o que um sistema consumidor precisa saber para falar com o zapgw — **em qualquer
linguagem**. Ele descreve o estado atual; quando o código mudar, este arquivo muda no mesmo commit.

**Ele foi escrito para ser suficiente sozinho.** Tudo o que você precisa para integrar e operar está
aqui: os corpos, os erros, as garantias, os limites e as três provas que você consegue executar sem
nós (o vetor de teste da assinatura, o teste de fumaça e a leitura de estado). Você não precisa do
código-fonte do gateway, e nenhuma instrução deste arquivo depende de você tê-lo.

## 🔴 Leia estas quatro coisas antes de qualquer outra

### 1. Não existe canal de suporte — este documento é o suporte

Não há endereço de e-mail, ticket, chat nem lista de anúncios. **Nada neste arquivo pede que você
"avise", "peça" ou "confira com a gente"**: onde houver uma decisão a tomar, ela está escrita aqui de
forma que você a execute sozinho.

Existe **uma** pessoa do outro lado, e o alcance dela é estreito: **quem te entregou o slug**. Ela
resolve o que só ela pode resolver — reabrir a janela de cadastro de 24 h, rotacionar um token de
consumidor que vazou, ou dizer o que aconteceu com uma instância que sumiu. Ela **não** é canal de
dúvida, de pedido de campo novo nem de investigação de tráfego.

**Mudança que quebra é anunciada num lugar só: a seção *Mudanças que quebram*, no fim deste
arquivo.** Ele é versionado no git junto com o código, e a forma de não ser surpreendido é relê-la
antes de subir uma integração nova. Não há aviso por outro canal, e não há endpoint que declare
versão de formato.

### 2. Hoje só o webhook é alcançável de fora da rede do gateway

**Isto é DECISÃO de quem opera o gateway, não obra inacabada — e ela decide como você começa.** De
todas as rotas descritas aqui, **apenas `POST /v1/inbound/{slug}`** (o webhook que a **Meta** chama)
está publicada na internet. Todo o resto — cadastro, envio, fumaça, pausa, leituras, estado,
templates, mídia, saúde — responde **só na LAN do gateway**, num entrypoint interno, **e continua
assim até haver necessidade que justifique mudar**. Não existe data, e não haverá anúncio de uma.

Consequência prática: **um integrador que não esteja dentro da rede do gateway não alcança o
`POST /v1/cadastro`**, que é por onde passam o `app_secret` e o `token_envio` **dele**. Quem cadastra
é quem alcança a rede — na prática, você entrega os seus valores a quem te passou o slug e essa
pessoa executa o cadastro. **O resto do fluxo continua sendo seu**, do teste de fumaça em diante.

Este documento descreve as rotas como elas são, não como elas seriam se estivessem publicadas.

### 3. As convenções dos exemplos, para você não confundir exemplo com valor real

Todo exemplo deste arquivo usa **os mesmos marcadores fictícios**, e nenhum deles existe:

| Marcador | Valor usado | O que é |
|---|---|---|
| slug de instância | `lojinha` | o slug **fictício** dos exemplos. O seu vem no pacote de entrega, e é outro |
| telefone (forma canônica) | `5511999990000` | número **sintético**, com o 9º dígito |
| telefone (como a Meta às vezes manda) | `551199990000` | o **mesmo** número sem o 9º dígito — é o par que a seção `de_cru`/`de_canonico` explica |
| ids da Meta | `PNID_TESTE`, `WABA_TESTE`, `wamid.TESTE001`… | identificadores de teste, nunca reais |
| base das URLs que **você** chama | `https://zapgw.exemplo.com.br:8443` | **substitua pela URL de cadastro que você recebeu** (item 2 do pacote); a porta faz parte dela |
| URL que **a Meta** chama | `https://zapgw.exemplo.com.br/v1/inbound/lojinha` | é o item 3 do pacote, e é a única publicada na internet — porta 443, sem porta explícita |

**Se um valor de exemplo parecer real, ele não é.** Slug, telefone e id você tira do seu pacote de
entrega e da sua conta Meta — nunca daqui.

### 4. Como este documento marca o que é medido e o que é suposto

Onde uma afirmação vem de **tráfego real**, o texto diz o tamanho da medição ("medido em 267 payloads
reais"). Onde vem da **documentação da Meta**, diz a página e a data de leitura. Onde **não foi
conferido**, diz isso em voz alta em vez de suavizar. Trate as três como coisas diferentes: a
primeira você pode usar como fato, a segunda como intenção declarada de um terceiro, a terceira como
aviso de que ali você é quem vai descobrir.

---

## O que você recebe ao ser provisionado

A instância é criada **manualmente** por quem opera o gateway, e ele fornece o **slug** — ele é
imutável e vira caminho de URL. Só isso: ele **não conhece, não guarda e não pede** os dados da sua
conta Meta. Quem os cadastra é você, pela rota da seção seguinte.

Confira que recebeu **os cinco itens**. A conversa acontece uma vez e o gateway não tem canal para
avisar ninguém — item faltando aqui vira trabalho parado do seu lado:

| # | O que | Para que serve |
|---|---|---|
| 1 | o **slug** da sua instância | ele identifica a instância em **todo** corpo de requisição (`"instancia": "<slug>"`) |
| 2 | a URL de **cadastro** (`POST …/v1/cadastro`) | é por onde você cadastra a sua Meta |
| 3 | a URL de **webhook** (`…/v1/inbound/<slug>`) | é o que **você** cola em *Callback URL* no painel da Meta **da sua conta** |
| 4 | `verify_token` e `segredo_entrega` | o primeiro você digita em *Verify Token* no painel da Meta; o segundo confere o HMAC de cada entrega que o gateway te faz (veja a obrigação 3) |
| 5 | o **token de consumidor** | é o `Authorization: Bearer` de toda chamada sua |

Os dois valores do item 4 são **sorteados pelo gateway e mostrados uma vez** — ele guarda só o
cifrado e não os mostra de novo. Se você não os recebeu, **volte a quem te entregou o slug antes de
começar** (é um dos poucos assuntos que só ele resolve): sem eles a verificação do webhook na Meta
não passa, e a assinatura das entregas fica impossível de conferir.

**Não falta mais nada.** Estes cinco itens são o pacote inteiro — não existe um sexto valor que
alguém tenha esquecido de te mandar, e nenhuma seção deste documento vai pedir um. Tudo o que não
está na lista ou é **seu** (os dados da sua conta Meta, que você mesmo cadastra na seção seguinte) ou
está escrito aqui.

**O que você NÃO recebe, e não deve pedir:** nada da conta Meta de terceiros, e nenhum valor de
volta do que você mesmo cadastrar. Segredo entra e não volta — veja a seção do cadastro.

> ℹ️ **Um valor que NÃO viaja no pacote e que você vai querer conhecer: o `timeout_ms` da sua
> instância.** É o prazo que o gateway se dá para falar com a Meta numa chamada sua, gravado por
> instância. **Ele não é exposto em rota nenhuma hoje** — nem no `GET /v1/estado` —, e o default é
> **5000 ms**. Como a regra que importa é *"o timeout do seu cliente HTTP tem de ser MAIOR que o do
> gateway"*, você não precisa do número exato: use um prazo com folga (30 s é confortável) e você
> recebe a resposta do gateway em vez de um erro seu. O caso em que a diferença aparece está descrito
> em *Falhou: a sua chave volta a valer, ou não?*.

## O que cada segredo permite a quem o roubar

Escrito porque a proteção do seu lado é problema seu, e para dimensionar você precisa saber o que
cada valor vale na mão de outra pessoa:

| Segredo | Quem o tem consegue |
|---|---|
| **token de consumidor** | 🔴 mandar mensagem pela sua instância **e RECONFIGURÁ-LA** — veja o alerta abaixo |
| `segredo_entrega` | forjar uma entrega que o seu sistema vai aceitar como vinda do gateway |
| `verify_token` | re-verificar um webhook na Meta; sozinho, não move tráfego |
| `app_secret`, `token_envio` | são **seus**, da sua conta Meta: quem os tem fala com a Meta como você, dentro e fora do gateway |

🔴 **O token de consumidor deixou de ser só "manda mensagem".** Com a rota de cadastro, quem o
roubar **repõe as credenciais e aponta a sua instância para a Meta dele** — mensagens passariam a
sair pelo número dele, e as entregas iriam para a `callback_url` dele. É consequência aceita do
modelo (é o que permite você se virar sozinho, sem nos perguntar nada), e o que a limita **não é
permissão, é tempo**: passada a janela de 24 h da seção seguinte, um token roubado volta a valer só
"manda mensagem", que é o risco que sempre existiu.

**O que isso exige de você:** trate o token de consumidor como credencial de administração enquanto
a janela estiver aberta — não o deixe em repositório, em log, em transcript de terminal nem no
navegador de um painel público. Se ele vazar, **volte a quem te entregou o slug na hora** — é o
segundo dos assuntos que só ele resolve: existe um comando de rotação do lado do gateway, e o token
anterior deixa de autenticar no mesmo instante.

## Cadastrar a SUA Meta — `POST /v1/cadastro`

**A direção é você → gateway, e é escrita.** O gateway não devolve configuração: ele recebe.

```
POST /v1/cadastro
Authorization: Bearer <token de consumidor>
Content-Type: application/json
```

```jsonc
{
  "instancia":       "lojinha",          // o slug que você recebeu
  "waba_id":         "…",                // Business Settings → contas do WhatsApp
  "phone_number_id": "…",                // o id do NÚMERO na Graph API (não é o telefone)
  "numero_exibido":  "5511999990000",    // o telefone como ele aparece para o cliente
  "app_secret":      "…",                // App → Configurações → Básico → Chave Secreta
  "token_envio":     "…",                // token PERMANENTE de System User
  "callback_url":    "https://…",        // para onde entregamos o que a Meta mandar
  "bundle_ca":       ""                  // opcional: PEM da SUA CA, se você não usa CA pública
}
```

**O cadastro SUBSTITUI: mande sempre o conjunto completo.** Campo omitido vale como **vazio**, e não
como "não mexa". Isso é deliberado — um cadastro parcial exigiria que você soubesse o que já está
gravado, e é exatamente isso que o gateway nunca devolve. Recadastrar é o seu caminho de **rotação**:
trocou o `app_secret` no painel da Meta, mande o conjunto de novo.

Dois campos podem ir **vazios**, e vazio é estado legítimo:

- `callback_url` vazia = **instância só de saída**: o gateway não entrega a ninguém o que a Meta
  mandar. Não é erro, é uma escolha;
- `bundle_ca` vazio = a entrega usa a store de CAs do sistema (o caso normal). Preencha só se o seu
  endpoint usa **CA própria** — isso troca a âncora de confiança, e **não** desliga verificação
  nenhuma. Não existe modo de não verificar certificado neste gateway, em caminho nenhum.

A `callback_url` tem de ser `https://` pela razão da seção *A sua `callback_url` tem de ser
`https://`*, mais abaixo.

### O `200`: o que ficou gravado, sem devolver nada do que você mandou

```jsonc
{
  "instancia": "lojinha",
  "estado": "pausada",
  "pausada": true,
  "janela_de_cadastro": {
    "aberta": true,
    "primeira_insercao_em": "2026-07-20T09:00:00Z",
    "fecha_em": "2026-07-21T09:00:00Z"
  },
  "cifrados": [
    { "campo": "app_secret",      "cadastrado": true  },
    { "campo": "verify_token",    "cadastrado": true  },
    { "campo": "token_envio",     "cadastrado": true  },
    { "campo": "callback_url",    "cadastrado": true  },
    { "campo": "segredo_entrega", "cadastrado": true  },
    { "campo": "bundle_ca",       "cadastrado": false }
  ],
  "proximo_passo": "esta instancia continua PAUSADA…"
}
```

🔴 **Segredo entra e não volta — e esta rota não abre exceção.** A resposta diz apenas **se** cada
campo está cadastrado, nunca o valor: nem inteiro, nem truncado, nem em hash. Um endpoint que
devolvesse credencial transformaria um token de consumidor vazado em roubo da **sua** conta Meta.
Use `cifrados` para conferir que o conjunto chegou inteiro — em particular que `callback_url` está
`true` se você espera receber entregas.

🔴 **Cadastrar NÃO ativa.** A instância continua `pausada`, e enquanto estiver assim o webhook
responde `503` e o envio também. Só um **envio bem-sucedido** ativa (o teste de fumaça): chame
`POST /v1/fumaca` (seção seguinte) com esta instância e o destino que deve **receber** a mensagem de
teste. Cadastrar não prova nada — enviar prova. Se cadastrar ativasse, uma credencial errada viraria
instância "ativa" que recusa tudo.

### A janela de 24 h — o que ela é, e como não perdê-la

Você pode escrever a configuração da sua instância por **24 h contadas da PRIMEIRA vez que você
cadastrou algo** — não da criação da instância (você pode demorar cinco dias para começar, e o
relógio só parte quando você escreve) e **não reiniciando a cada mudança** (senão quem mexesse todo
dia manteria a janela aberta para sempre). Durante ela, recadastre à vontade: é assim que você testa,
erra e corrige sozinho.

Um cadastro **recusado** (`400`) **não** abre a janela — errar o corpo na primeira tentativa não
custa as suas 24 h.

Depois da janela, o `POST` responde **`409`** e nada é gravado; a configuração que já estava lá
continua valendo. **A saída é humana, existe, e é o terceiro dos assuntos que só quem te entregou o
slug resolve:** peça a ele para reabrir a janela — existe comando para isso do lado do gateway.
Reabrir devolve o prazo e **não altera a configuração** (quem cadastra continua sendo você), e o
relógio novo só parte quando você voltar a escrever.

> ⚠️ **Planeje o uso da janela, porque ela é o único período em que você conserta sozinho.** O caso
> que mais bate nela: `token_envio` errado ou sem permissão, descoberto no `POST /v1/fumaca` **depois**
> de a janela ter fechado. Enquanto ela estiver aberta, o ciclo *cadastra → fumaça → corrige →
> recadastra* é todo seu; depois dela, cada correção de credencial custa uma ida a outra pessoa.
> **Rode o fumaça logo após o primeiro cadastro**, não no dia seguinte.

### Erros

| HTTP | `classe` | Quando |
|---|---|---|
| `400` | `permanente` | corpo não é JSON, falta `instancia`, falta um campo obrigatório, `callback_url` fora de `https`, `bundle_ca` sem certificado. A mensagem **nomeia o campo** e nunca ecoa o valor |
| `401` | `config` | sem `Authorization`, ou token que ninguém reconhece |
| `403` | `config` | o token é válido, mas essa instância **não é sua** |
| `404` | `config` | a instância não existe mais no gateway (fale com quem te entregou o slug) |
| `409` | `config` | **a janela de cadastro fechou**; nada foi gravado |
| `413` | `permanente` | corpo acima do teto (**1 MiB** por default — ver *Limites conhecidos*) |
| `503` | `retentavel` | o gateway não conseguiu falar com o próprio banco; repetir é seguro |

## Provar o canal — `POST /v1/fumaca` (2026-07-28)

É o passo que **ativa** a sua instância. Ele manda uma mensagem de TESTE de verdade para um número que
você escolhe, e só ativa se a Meta aceitar o envio — cadastrar nunca ativa (seção anterior).

```
POST /v1/fumaca
Authorization: Bearer <token de consumidor>
Content-Type: application/json
```

```jsonc
{
  "instancia": "lojinha",
  "destino":   "5511999990000"    // numero que vai RECEBER a mensagem de teste, em E.164
}
```

🔴 **`destino` não tem valor default, de propósito.** Um default aqui mandaria a mensagem de teste
para um número escolhido por nós, não por você — e mensagem enviada não se desfaz.

### O `200`

```jsonc
{
  "instancia": "lojinha",
  "estado": "ativa",
  "pausada": false,
  "ja_estava_ativa": false,
  "wa_message_id": "wamid.HBgL…",
  "ativa_desde": "2026-07-28T21:40:00Z"
}
```

🔴 **`ativo = 1` é SEMPRE consequência de a Meta ter aceitado o envio, nunca de você ter chamado a
rota.** Se a Meta recusar (credencial errada, número inválido, o que for), a instância **continua
pausada** e a chamada devolve erro — veja a tabela de erros abaixo. Não existe, nesta rota nem em
nenhuma outra, um jeito de ativar sem uma mensagem de teste ter saído de verdade.

🔴 **Instância já ativa: esta rota NÃO manda mensagem.** A resposta vem com `ja_estava_ativa: true`,
`wa_message_id` ausente, e `ativa_desde` diz desde quando ela está ativa (o carimbo da mensagem de
teste que a ativou). *Chamar `POST /v1/fumaca` de novo numa instância já ativa é seguro e barato —
ela nunca gasta uma segunda mensagem paga só porque você chamou a rota outra vez.* Se você precisa
mesmo de uma prova nova (por exemplo depois de trocar o `token_envio`), pause primeiro
(`POST /v1/pausa`, a seguir) e rode o fumaça de novo.

### Erros

| HTTP | `classe` | Quando |
|---|---|---|
| `400` | `permanente` | corpo não é JSON, falta `instancia` ou `destino` |
| `401` | `config` | sem `Authorization`, ou token que ninguém reconhece |
| `403` | `config` | o token é válido, mas essa instância **não é sua** |
| `404` | `config` | a instância não existe mais no gateway (fale com quem te entregou o slug) |
| `502` | `config` \| `permanente` \| `retentavel` \| `desconhecido` | a Meta recusou o envio ou não respondeu — a instância continua PAUSADA. A mensagem diz por quê, pela mesma classificação do `POST /v1/messages` (seção *O que cada erro quer dizer*) |
| `413` | `permanente` | corpo acima do teto (**1 MiB** por default — ver *Limites conhecidos*) |

## Pausar o canal — `POST /v1/pausa` (2026-07-28)

O sentido seguro: tira a sua instância do ar sem apagar nada, sem exigir prova nenhuma.

```
POST /v1/pausa
Authorization: Bearer <token de consumidor>
Content-Type: application/json
```

```jsonc
{ "instancia": "lojinha" }
```

```jsonc
// 200
{ "instancia": "lojinha", "estado": "pausada", "pausada": true }
```

Enquanto pausada, o webhook responde `503` e o envio também — a Meta reenfileira o que chegar e
reenvia por até 36 h. **A volta exige um fumaça novo** (seção anterior): não existe nenhum outro
caminho para reativar.

### Erros

| HTTP | `classe` | Quando |
|---|---|---|
| `400` | `permanente` | corpo não é JSON, ou falta `instancia` |
| `401` | `config` | sem `Authorization`, ou token que ninguém reconhece |
| `403` | `config` | o token é válido, mas essa instância **não é sua** |
| `404` | `config` | a instância não existe mais no gateway (fale com quem te entregou o slug) |
| `413` | `permanente` | corpo acima do teto (**1 MiB** por default — ver *Limites conhecidos*) |
| `503` | `retentavel` | o gateway não conseguiu falar com o próprio banco; repetir é seguro |

---

## O que você recebe

O zapgw faz um `POST` na `callback_url` da sua instância:

```
Content-Type: application/json
X-Zapgw-Signature:      sha256=<hex>    HMAC-SHA256 do TIMESTAMP + do corpo — veja a obrigação 3
X-Zapgw-Timestamp:      <unix>          segundos; entra na assinatura, e é o que dá o anti-replay
X-Zapgw-Event-Id:       <id>            id do PRIMEIRO evento do lote — leia a ressalva
X-Zapgw-Correlation-Id: <id>            atravessa os dois lados; cite-o ao relatar problema
X-Hub-Signature-256:    sha256=<hex>    a assinatura ORIGINAL da Meta, repassada
```

```jsonc
{
  "instancia": "lojinha",
  "recebido_em": "2026-07-23T14:05:00Z",
  "cru": "<os bytes EXATOS que a Meta enviou, em base64>",
  "eventos": [
    { "tipo": "mensagem",
      "id": "msg:wamid.ABC",
      "phone_number_id": "…", "waba_id": "…",
      "wa_message_id": "wamid.ABC",
      "sub_tipo": "text",
      "de_cru": "551199990000",
      "de_canonico": "5511999990000",
      "nome_contato": "…",
      "texto": "…",
      "responder_a": "wamid…",
      "encaminhada": true, "encaminhada_muitas_vezes": true,
      "botao_payload": "…", "botao_texto": "…",
      "reacao": { "emoji": "👍", "alvo": "wamid…" },
      "midia_id": "…", "midia_mime_payload": "audio/ogg; codecs=opus",
      "voz": true,
      "legenda": "…", "nome_arquivo": "…",
      "localizacao": { "latitude": 37.44, "longitude": -122.16, "nome": "…", "endereco": "…" },
      "timestamp": 1769000000 }
  ],
  "parse_error": ""
}
```

> **Dois campos deste exemplo já mentiram, e os dois quebram código, não só leitura.**
> `parse_error` é a string **vazia** quando não houve erro, nunca `null` e nunca ausente — ele **não**
> é omitido, e **isso nunca mudou**. E `eventos`, quando o gateway não enriqueceu nada, é hoje **`[]`**
> — array vazio, nunca `null`: a normalização é feita num lugar só, na montagem do envelope, e travada
> por um teste que afirma sobre os **bytes do fio**, não sobre a estrutura em memória.
>
> ⚠️ **Até 2026-07-28 o fio mandava `"eventos": null` neste caso** — desde o primeiro dia do gateway
> (2026-07-23). Se você escreveu `envelope.get("eventos") or []` (ou `?? []`) por causa daquele
> aviso, **mantenha**: a defesa continua correta e continua necessária, porque `[]` também é falso em
> Python e itera zero vezes em qualquer linguagem. Veja *Webhook de CONTA*, mais abaixo, e a entrada
> de 2026-07-28 em *Mudanças que quebram*.

**Quais campos vêm sempre, e quais só aparecem quando há o que dizer.** A regra que vale para você é
"trate tudo como opcional", mas ela tem exceções nomeadas, e escondê-las seria trocar uma imprecisão
por outra:

| Nível | Vêm **SEMPRE**, mesmo vazios | Todo o resto |
|---|---|---|
| envelope | `instancia`, `recebido_em`, `cru`, `eventos`, `parse_error` | — |
| um item de `eventos` | `tipo` e `id` | omitido quando não há valor |
| dentro dos blocos aninhados | `reacao.alvo`; `localizacao.latitude` e `.longitude`; `erro.codigo` e `.mensagem`; `cobranca.categoria`; `template.estado`; `template_categoria.categoria_nova` | omitido quando não há valor |

Fora dessas linhas, **todo campo é omitido quando vazio**: uma mensagem de texto simples não ganha
`reacao`, `voz`, `legenda`, `nome_arquivo`, `localizacao`, `responder_a`,
`encaminhada`/`encaminhada_muitas_vezes` (desde 2026-07-28) nem `erro` (desde 2026-07-26). É o que a
garantia "o envelope só cresce" (abaixo) exige, e há teste de não-regressão preso nos campos atuais.

> ⚠️ **Os campos da primeira coluna vêm sempre porque a ausência deles seria ambígua ou fatal** —
> `id` é a sua chave de dedup, `latitude: 0` é uma coordenada válida, `erro.codigo: 0` teria de ser
> distinguível de "sem código". **Isso não é licença para você exigir presença**: um bloco que a Meta
> mande num formato ilegível é descartado inteiro, e aí o evento chega sem ele (veja as entradas de
> 2026-07-28 em *Mudanças que quebram*). A tabela diz o que o gateway garante **quando produz o
> campo**, não que o campo exista em todo cenário.

### `responder_a` — a que mensagem esta responde

`responder_a` traz o `wamid` da mensagem **citada**, quando o remetente respondeu segurando a bolha
de outra mensagem (a Meta manda isso em `messages[].context.id`, em **qualquer** `sub_tipo`, não só
em `text`). **MESMO NOME do campo equivalente no envio** (`responder_a` no `POST /v1/messages`, veja
mais abaixo) — o referente é idêntico nos dois sentidos: o `wamid` da mensagem citada. Enviar e
receber com nomes diferentes para a mesma coisa seria o começo de dois vocabulários.

**A Meta também manda `context.from` (o número do NEGÓCIO que enviou a mensagem original), e o
gateway não repassa esse campo.** Não é descuido: um campo que parece "de quem" e é "para quem" é
convite a bug, e ninguém pediu esse valor até hoje. Se um dia alguém precisar dele, entra com um
nome que diga o que é — nunca reaproveitando `responder_a` para dois sentidos.

Ausente quando a mensagem não é resposta a nada — a chave some do JSON, nunca `"responder_a": ""`.

> ⚠️ **Ausente NÃO significa "não é resposta". Significa "a Meta não mandou vínculo".**
> Observado em 2026-07-26 (dois payloads reais da mesma conversa com 3 minutos de diferença):
> quem responde **segurando a bolha** gera `context` e você recebe `responder_a`; quem responde
> **digitando na notificação do celular** — resposta *inline* — gera um payload **sem `context`
> nenhum**, e portanto sem `responder_a`, ainda que seja uma resposta de verdade para a pessoa que
> escreveu.
>
> Isso não é o gateway omitindo nem o seu lado perdendo: **a Meta não manda o vínculo nesse caso.**
> E responder pela notificação é o jeito mais rápido no celular, então a ausência provavelmente é a
> maioria do tráfego, não a exceção.
>
> **Consequência prática:** não trate `responder_a` ausente como anomalia, não alarme por causa
> disso, e não construa nada que dependa de toda resposta trazer o vínculo. Se um dia alguém abrir
> uma investigação por "`responder_a` faltando num payload real", a resposta é esta linha.
>
> **Desde 2026-07-28 há uma terceira razão para a ausência, e ela é nossa, não da Meta:** se o bloco
> `context` chegar com um **tipo** que o gateway não consegue ler, ele descarta o bloco inteiro e
> entrega a mensagem sem `responder_a`, `encaminhada` e `encaminhada_muitas_vezes` — em vez de
> perder a mensagem, que era o que acontecia antes. Veja *Mudanças que quebram*, no fim deste
> arquivo. Para você o efeito é indistinguível dos outros dois casos, e a orientação acima não muda.

**Exemplo executado** (o parser do gateway sobre um payload de **captura real**, 2026-07-26: alguém
respondeu citando uma mensagem anterior; `context.from` e `context.id` vieram diferentes um do
outro no payload real, confirmando que é o `id` que vira `responder_a`, não o `from`):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE015",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000013,
  "wa_message_id": "wamid.TESTE015",
  "sub_tipo": "text",
  "de_cru": "551199990000",
  "de_canonico": "5511999990000",
  "nome_contato": "Fulana de Teste",
  "texto": "Recebido",
  "responder_a": "wamid.TESTE001"
}
```

### `encaminhada` e `encaminhada_muitas_vezes` — a mensagem não foi escrita agora, para você

Os dois vêm de `messages[].context` na Meta (`forwarded` e `frequently_forwarded`) e chegam em
**qualquer** `sub_tipo`, não só em `text`.

- **`encaminhada`** — a pessoa repassou uma mensagem em vez de escrevê-la. Sinal fraco e comum:
  cliente encaminhando foto de referência, tabela de preço, print de conversa. É **contexto**, não
  alarme.
- **`encaminhada_muitas_vezes`** — é o sinal de **corrente** do WhatsApp (a marca "encaminhada muitas
  vezes" que o app mostra). **É este que muda decisão automática.** Se o seu lado responde sozinho —
  detecção de agendamento, resposta automática, relay —, uma corrente hoje é tratada como se a
  cliente tivesse escrito aquilo. Este campo é o que permite **não** disparar fluxo de negócio em
  cima dela.

**São booleanos simples, e a ausência é `false` — ao contrário de `voz`.** A distinção existe e é
deliberada, então vale escrever qual é: em `voz`, "a Meta disse que não é nota de voz" e "a Meta não
disse nada" levam você a fazer coisas **diferentes** ao reenviar o áudio, e por isso ausente ≠
`false` lá. Aqui não há terceira ação: "não foi encaminhada" e "a Meta não disse que foi" levam ao
mesmo lugar — tratar a mensagem como escrita pela pessoa. Por isso **a chave só aparece quando é
`true`**; você nunca vai receber `"encaminhada": false`, e não deve tratar a ausência como
desconhecido.

> ⚠️ **Não há captura real destes campos até 2026-07-28.** Nenhum dos payloads que os consumidores
> guardaram traz `forwarded`, e a documentação pública da Meta, procurada nessa data, não tem mais
> página descrevendo os campos de `context`. O gateway lê os dois campos onde a Meta historicamente
> os coloca, e o fixture que prova a leitura é **sintético**
> (marcado como sintético no inventário de
> corpus).
>
> **Consequência prática, e é a razão de este aviso existir:** trate a **presença** dos campos como
> informação boa, e a **ausência** como "não recebemos o sinal" — não como prova de que a mensagem é
> original. **A prova que vale para você é a sua**: se você observar uma mensagem encaminhada de
> verdade chegando com `encaminhada: true`, está confirmado no seu tráfego, e é isso que decide o que
> o seu código pode assumir. Enquanto não observar, não construa nada que dependa da presença.

**`referred_product`, o terceiro campo de `context`, continua fora do envelope.** Ninguém pediu, e o
envelope só cresce — acrescentar depois é de graça, tirar depois é quebra.

**Encaminhar não é citar.** Uma mensagem encaminhada que não responde a nada chega com `context`
**sem `id`**, ou seja, com `encaminhada` e **sem** `responder_a`. Os dois casos são independentes e
podem aparecer juntos.

**Exemplo executado** (o parser do gateway sobre um payload **sintético** — ver o aviso acima):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE018",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000021,
  "wa_message_id": "wamid.TESTE018",
  "sub_tipo": "text",
  "de_cru": "551199990000",
  "de_canonico": "5511999990000",
  "nome_contato": "Fulana de Teste",
  "texto": "Tabela de precos 2026",
  "encaminhada": true
}
```

Note que `encaminhada_muitas_vezes` **não aparece**: neste payload ela é `false`, e `false` some do
envelope.

### `reacao`, `voz`, `legenda`/`nome_arquivo`, `localizacao`, `erro` — o que cada um significa

Estes campos existem para que você **não precise reparsear o `cru`** para reação, localização,
legenda, nome de arquivo, nota de voz e — desde 2026-07-26 — o motivo de a Meta não saber representar
uma mensagem. Antes deles, o `sub_tipo` chegava rotulado (`"reaction"`, `"location"`,
`"unsupported"`) mas sem o CONTEÚDO do evento, e a única forma de recuperar o dado era reimplementar
o parser da Meta por conta própria.

- **`reacao` (`sub_tipo: "reaction"`)** — `emoji` e `alvo` (o `wamid` da mensagem reagida).
  **`emoji` ausente é remoção, não erro**: quando alguém tira a reação que tinha posto, a própria
  Meta manda o evento sem a chave `emoji` — é assim que ela distingue "reagi" de "tirei a reação".
  O gateway repassa essa ausência tal como veio; `alvo`, ao contrário, é sempre obrigatório: um
  evento de reação sem ele é contado como erro de parse (`parse_error`) e não vira `reacao`.
- **`voz` (`sub_tipo: "audio"`)** — `true`/`false`/**ausente**, nesta ordem de significado. É a nota
  de voz tocável (obrigação 5 do contrato, mais abaixo) versus anexo de áudio comum. **Ausente não é
  `false`**: a Meta às vezes não manda o campo `voice` no payload, e tratar isso como `false` por
  default inventaria um dado que você não tem. Se `voz` não aparecer, o gateway não sabe.
- **`legenda` e `nome_arquivo` (mídia com `caption`/`filename` no payload da Meta)** — a legenda
  existe em imagem, vídeo e documento; o nome de arquivo só em documento. Os dois se referem à
  mesma mídia do `midia_id` no mesmo evento.
- **`localizacao` (`sub_tipo: "location"`)** — `latitude`, `longitude` (sempre presentes, mesmo
  quando `0` — o cruzamento do meridiano de Greenwich com o equador é uma coordenada válida) e,
  quando o remetente os mandou, `nome` e `endereco`.
- **`erro` (`sub_tipo: "unsupported"`, 2026-07-26)** — **MESMO campo e MESMA forma** do
  `erro` do evento de `status` (descrito logo abaixo: `codigo`, `mensagem`, `detalhes`), mas com
  **SIGNIFICADO diferente**, e a diferença importa: no evento de status, `erro` quer dizer "a
  entrega falhou"; aqui quer dizer **"a Meta recebeu algo e a Cloud API não sabe representar"** — a
  Meta não falhou em entregar nada, ela entregou e não soube decodificar o conteúdo (o caso
  observado é o código `131051`, `"Message type unknown"`). Sem este campo, uma mensagem
  `unsupported` chegava com `sub_tipo` e um `id`, e **nada mais** — indistinguível de "mensagem
  vazia" para quem só olha o envelope. Ausente quando a Meta não mandou `errors[]` na mensagem —
  omitido, nunca `{"codigo": 0, "mensagem": ""}` (mesma regra do lado do status, ver abaixo).

**Exemplos executados** (desserializados e revalidados contra o parser antes de entrar aqui —
o parser do gateway sobre os payloads do corpus):

Reação adicionada (**captura real**, 2026-07-26; o
emoji `❤️` tem dois codepoints, `U+2764` + `U+FE0F` variation selector, de propósito: um emoji de
um codepoint só não prova que o parser preserva o par):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE006",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000005,
  "wa_message_id": "wamid.TESTE006",
  "sub_tipo": "reaction",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "reacao": { "emoji": "❤️", "alvo": "wamid.TESTE001" }
}
```

Reação **removida** (**captura real**,
2026-07-26: mesma reação do exemplo acima, desfeita 20 segundos depois, mesmo alvo) — note a
ausência da chave `emoji` dentro de `reacao`, não uma string vazia:

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE007",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000006,
  "wa_message_id": "wamid.TESTE007",
  "sub_tipo": "reaction",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "reacao": { "alvo": "wamid.TESTE001" }
}
```

Localização (**captura real**, 2026-07-26;
coordenadas arredondadas de propósito na captura, para não expor a localização real). Este é o
**pin solto** — sem `nome`/`endereço` — que é o caso observado como comum; a Meta também aceita um
pin de estabelecimento com os dois campos preenchidos (ver a nota acima sobre `nome`/`endereco`
serem opcionais), mas isso não é o que a maioria dos usuários manda:

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE008",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000007,
  "wa_message_id": "wamid.TESTE008",
  "sub_tipo": "location",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "localizacao": {
    "latitude": -21.229,
    "longitude": -43.7892
  }
}
```

Documento com legenda e nome de arquivo:

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE009",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000008,
  "wa_message_id": "wamid.TESTE009",
  "sub_tipo": "document",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "midia_id": "MEDIA_TESTE2",
  "midia_mime_payload": "application/pdf",
  "legenda": "meu recibo",
  "nome_arquivo": "recibo-teste.pdf"
}
```

Nota de voz — `voz: true` (o mesmo payload da armadilha
dos dois mimes citada na obrigação 5):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE004",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000003,
  "wa_message_id": "wamid.TESTE004",
  "sub_tipo": "audio",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "midia_id": "MEDIA_TESTE",
  "midia_mime_payload": "audio/ogg; codecs=opus",
  "voz": true
}
```

`sub_tipo: "unsupported"` com o motivo (2026-07-26) — a Meta recebeu algo que a Cloud API
não sabe representar (o exemplo abaixo é sintético, com os valores do exemplo oficial da Meta:
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/unsupported/,
lido em 2026-07-26; não há corpus de captura real para este caso ainda):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE016",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000014,
  "wa_message_id": "wamid.TESTE016",
  "sub_tipo": "unsupported",
  "de_cru": "5511999990000",
  "de_canonico": "5511999990000",
  "erro": {
    "codigo": 131051,
    "mensagem": "Message type unknown",
    "detalhes": "Message type is currently not supported."
  }
}
```

Mensagem de texto simples, para comparação — **nenhum dos cinco campos novos aparece**
(captura real):

```json
{
  "tipo": "mensagem",
  "id": "msg:wamid.TESTE001",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000000,
  "wa_message_id": "wamid.TESTE001",
  "sub_tipo": "text",
  "de_cru": "551199990000",
  "de_canonico": "5511999990000",
  "nome_contato": "Fulana de Teste",
  "texto": "mensagem de teste"
}
```

### `status` (`tipo: "status"`) — o que cada campo significa

O evento de status confirma o destino de uma mensagem que **você** mandou: `sent` → `delivered` →
`read`, ou `failed`. Ele não tem `sub_tipo`; os campos que importam são estes:

| Campo | O que é |
|---|---|
| `status` | `sent`, `delivered`, `read` ou `failed` — o vocabulário é da Meta, repassado sem tradução |
| `wa_message_id` | o id da mensagem que você mandou (o mesmo que veio na resposta do `POST /v1/messages`) |
| `para_cru` / `para_canonico` | o destinatário, nas duas formas — mesma razão do `de_cru`/`de_canonico` da mensagem: a Meta não garante a mesma grafia que você cadastrou |
| `erro` | **só em `failed`, e só quando a Meta mandou o motivo** — ver abaixo |
| `cobranca` | **quando a Meta mandou `pricing`** — sob qual categoria ela cobrou esta entrega, ver abaixo |

> 🔴 **NÃO ORDENE O HISTÓRICO DE UMA MENSAGEM PELO `timestamp` — o `sent` e o `delivered` da MESMA
> mensagem chegam com o MESMO carimbo da Meta.** Não é hipótese: foi **medido em tráfego real** pelo
> um consumidor em 2026-07-28, e está congelado no corpus deste repositório —
> os dois fixtures congelados desse par são o
> mesmo `wa_message_id` com `timestamp` `1785072102` nos **dois**.
>
> O `timestamp` do envelope é o carimbo **da Meta** (`statuses[].timestamp`), repassado sem
> tradução. Ele diz *quando o evento aconteceu do ponto de vista dela*, e ela pode datar dois
> estados diferentes no mesmo segundo. Uma tela que ordene por ele mostra `delivered` antes de
> `sent` metade das vezes, sem nenhum erro em lugar nenhum.
>
> **O que separa os dois é a ORDEM DE CHEGADA** — o instante em que o `POST` bateu no seu endpoint,
> que é dado seu e não nosso. Grave-o (a obrigação 1 já manda gravar o `cru` com o momento do
> recebimento) e ordene por ele, com o `timestamp` da Meta como desempate ou como informação
> exibida, nunca como chave de ordenação.
>
> **A identidade de cada estado continua íntegra:** o `id` do evento é `status:{wamid}:{status}`, e
> `sent` e `delivered` do mesmo envio têm ids **diferentes**. O seu dedup não junta os dois — o que
> o carimbo igual quebra é a ORDEM, não a unicidade.

**`erro` (2026-07-26; `detalhes` acrescentado dias depois, no mesmo mês)** — `codigo` (inteiro),
`mensagem` (texto) e `detalhes` (texto, **opcional**). Existe porque `failed` sozinho não diz **por
quê**: sem o motivo, o operador humano que um sistema como o seu avisa quando uma entrega falha (o
gatilho original desta tarefa foi uma falha real, código `131026`, que ficou só gravada no banco
porque ninguém a viu) não tem o que mostrar.

> ⚠️ **`erro` é o MESMO campo, com a MESMA forma, em DOIS eventos diferentes — e o significado NÃO
> é o mesmo.** Desde 2026-07-26, `erro` também aparece no evento de **mensagem**
> (`sub_tipo: "unsupported"` — ver a seção acima). Aqui (status) ele quer dizer "a entrega falhou";
> lá (mensagem) quer dizer "a Meta recebeu algo e não soube representar" — a mensagem foi entregue,
> não falhou. Tudo o que este bloco descreve sobre `codigo`/`mensagem`/`detalhes`, sobre a lista
> `errors[]` e sobre "ausência é ausência, nunca zero" vale identicamente para os dois; **só o que
> `erro` está DIZENDO sobre o evento muda**, e é o `tipo`/`sub_tipo` do evento que diz qual dos dois
> é.

Formato confirmado em
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/
(lido em 2026-07-26): a Meta manda `errors[]` — uma lista — com `code`, `title`, `message`,
`error_data.details` e `href` por item. **O gateway repassa `codigo` (de `code`), `mensagem` (de
`title`) e `detalhes` (de `error_data.details`)** — não `message` (idêntico a `title` no exemplo da
doc), não `href`. Não há tradução para português: traduzir o código de um terceiro é decisão sua, e
uma tabela nossa apodreceria no dia em que a Meta acrescentasse um código novo.

**Por que `message` e `href` ficam de fora e `error_data.details` não.** A pergunta "algum
campo cortado faz falta?" foi levada a um consumidor real (2026-07-26, que conferiu o próprio
tradutor de erros antes de responder): `title` e `message` a Meta manda **iguais**
— mandar os dois repetiria a frase —, e `href` não tem consumidor. `error_data.details` é diferente
dos outros dois: é a **única** parte da mensagem que **acrescenta** informação em vez de repetir o
título. Sem ela, o aviso ao operador fica pobre exatamente nos casos em que o código genérico não
explica nada — que são os casos em que a mensagem faz falta.

**`detalhes` é opcional e pode faltar mesmo dentro de um `erro` presente.** A Meta só manda
`error_data` em parte dos códigos; quando falta, `codigo` e `mensagem` continuam saindo normalmente —
a ausência do objeto aninhado não derruba o resto do motivo. Quando falta, a chave `detalhes` some do
JSON (nunca `"detalhes": ""`) — mesma razão de `erro` inteiro ficar omitido em vez de zerado, ver
abaixo.

**`errors[]` pode trazer mais de um item; o gateway guarda só o PRIMEIRO.** Não é descarte em
silêncio — é a escolha registrada aqui: um único motivo já basta para o alerta a um operador, e não
há, até hoje, nenhum caso observado de itens conflitantes que justifique expor a lista inteira. Se
isso mudar, é mudança de contrato, não ajuste de detalhe.

**Ausência é ausência, nunca zero.** Um `failed` sem `errors[]` no payload da Meta — ou com um item
que o gateway não conseguiu interpretar — não ganha `erro`: o campo fica **omitido**, nunca
`{"codigo": 0, "mensagem": ""}`. Código `0` não é um código real da Meta; se o gateway o inventasse,
você não teria como distinguir "sem motivo relatado" de "erro genuíno de código zero".

### `cobranca` — sob qual categoria a Meta cobrou (2026-07-26)

**Não é curiosidade contábil — é o único jeito de saber, na PRIMEIRA mensagem, que a Meta
reclassificou um template.** Editar um template pode fazer a Meta reclassificar `UTILITY` →
`MARKETING`, o que muda preço e regras de envio. Sem este campo, isso só apareceria na **fatura**,
semanas depois: `pricing.category` é a Meta dizendo, **em cada entrega**, sob qual categoria ela
cobrou, e por isso a reclassificação aparece na primeira mensagem entregue depois da mudança, não
no fim do mês.

Pedido por um consumidor (2026-07-26), que conferiu antes de pedir: `pricing` não existia no contrato
nem no código deste gateway, mas **chega** — está em 145 dos 148 status que eles têm gravados. O
gateway o traduz para `cobranca`, no nosso vocabulário — o formato da Meta morre no gateway,
como em todo o resto do envelope, para que nenhum consumidor precise conhecer o formato dela para
saber quanto do seu tráfego é cobrado.

| Campo de `cobranca` | O que é |
|---|---|
| `categoria` | o `pricing.category` da Meta, repassado sem tradução: `utility`, `marketing`, `authentication`, `service`, entre outros que a Meta define |
| `cobravel` | o `pricing.billable` da Meta — `true`/`false`/**ausente** (ver a nota abaixo) |

**Só `categoria` e `cobravel` são modelados.** `pricing_model` e `type` — os outros dois campos que
a Meta manda dentro de `pricing` — ficam de fora até alguém dizer o que faria com eles: o envelope
só cresce, então acrescentar depois é de graça, tirar depois é quebra de contrato.

> ⚠️ **`cobravel` ausente e `cobravel: false` NÃO são a mesma informação — a diferença aqui é de
> DINHEIRO.** Mesma regra do `voz` (ver acima), com consequência maior: "a Meta disse que não
> cobra" (`false`) e "a Meta não disse nada" (ausente, a chave some do JSON) são coisas diferentes,
> e inventar `false` por default esconderia justamente o caso em que ninguém sabe se cobrou.

**`cobranca` inteiro fica ausente quando a Meta não mandou `pricing` no status** — o campo some do
JSON, nunca `{"categoria": "", "cobravel": false}`. Não invente um valor.

**E a ausência tem endereço: é no `sent`.** Medição de um consumidor sobre 225 payloads crus da Meta
(2026-07-28): dos **53 `sent`**, **49 vieram com `pricing` e 4 sem** (~7,5%); dos **49
`delivered`**, **49 vieram com** (100%). Um `sent` sem `cobranca` **não é falha nem payload
truncado — é caso normal**, e código que trate `cobranca` como garantida no `sent` quebra em cerca
de um a cada treze. **As duas formas estão congeladas lado a lado**, as duas com lastro de tráfego
real, e as duas aparecem como exemplo logo abaixo.

**Exemplo executado** (o parser do gateway sobre um payload de **captura real (parcial)**,
2026-07-26: o par `status`/`pricing` é literal, exatamente como chegou;
ele precisou de um envelope ao redor para virar fixture):

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE017:read",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000015,
  "wa_message_id": "wamid.TESTE017",
  "status": "read",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000",
  "cobranca": {
    "categoria": "utility",
    "cobravel": true
  }
}
```

**Exemplos executados** (o parser do gateway sobre os payloads do corpus):

Envio aceito **sem** `cobranca` (**captura real**,
2026-07-28: é um dos 4 `sent` em 53 que vieram sem `pricing`):

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE041:sent",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1785073298,
  "wa_message_id": "wamid.TESTE041",
  "status": "sent",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000"
}
```

Envio aceito **com** `cobranca` (**captura real**,
2026-07-28):

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE042:sent",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1785072102,
  "wa_message_id": "wamid.TESTE042",
  "status": "sent",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000",
  "cobranca": {
    "categoria": "service",
    "cobravel": false
  }
}
```

Entrega confirmada (**captura real**,
2026-07-28) — sem `erro`. **É o MESMO envio do exemplo acima**: repare no `wa_message_id`
igual, no `timestamp` **igual**, e no `id` do evento **diferente** — é exatamente o caso do aviso
🔴 no começo desta seção:

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE042:delivered",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1785072102,
  "wa_message_id": "wamid.TESTE042",
  "status": "delivered",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000",
  "cobranca": {
    "categoria": "service",
    "cobravel": false
  }
}
```

Falha, com o motivo (**captura real**,
2026-07-26: é a falha real de 2026-07-20 que motivou esta seção inteira do contrato; antes
era derivado do exemplo genérico da doc):

```json
{
  "tipo": "status",
  "id": "status:wamid.TESTE010:failed",
  "phone_number_id": "PNID_TESTE",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000009,
  "wa_message_id": "wamid.TESTE010",
  "status": "failed",
  "para_cru": "551199990000",
  "para_canonico": "5511999990000",
  "erro": {
    "codigo": 131026,
    "mensagem": "Message undeliverable",
    "detalhes": "Message Undeliverable."
  }
}
```

### `template_status` (`tipo: "template_status"`) — a Meta aprovou, rejeitou ou pausou um template (2026-07-26)

Este evento **não é sobre uma mensagem** e não tem destinatário: é a Meta avisando que o estado de um
**template** da sua conta mudou. Ele chega no mesmo `POST` de sempre, dentro de `eventos`.

**Por que ele importa mais do que parece: a `categoria` vem no evento.** A reclassificação `UTILITY`
→ `MARKETING` — que muda **preço e regras de envio** — aparece aqui **antes de qualquer mensagem ser
enviada**. É sinal mais precoce que o `cobranca` do status (acima), que só chega depois de uma
entrega. Os dois se complementam: este avisa que **mudou**, aquele confirma **o que foi cobrado**.

| Campo | O que é |
|---|---|
| `template.nome` / `template.idioma` | o par que identifica o template **no envio** (`template` e `idioma` do `POST /v1/messages`) — é por ele que você liga este aviso ao que você manda |
| `template.categoria` | `UTILITY`, `MARKETING`, `AUTHENTICATION` — o vocabulário é da Meta, repassado **sem tradução** |
| `template.estado` | `APPROVED`, `REJECTED`, `PENDING`, `PAUSED`, `DISABLED`… — também da Meta, sem tradução |
| `template.motivo` | o `reason` da Meta, **como veio** — inclusive a string `"NONE"`, ver abaixo |
| `waba_id` | a **única** chave de roteamento deste evento (ver a seção seguinte) |
| `timestamp` | o carimbo do lote (`entry.time` da Meta) — este webhook **não tem carimbo próprio** |

> ⚠️ **`"NONE"` é o valor NORMAL de `motivo`, não um erro nem um "vazio".** A Meta manda a string
> literal `"NONE"` quando não há motivo — não ausente, não `null`. O gateway repassa como veio, e
> **não traduz `"NONE"` para vazio**: "a Meta disse NONE" e "a Meta não mandou o campo" são fatos
> diferentes, e o segundo pode aparecer num tipo de evento que ainda não vimos. Se `motivo` sumir do
> JSON, é porque a Meta realmente não mandou o campo.
>
> **O mesmo campo, com a mesma doutrina, também está no catálogo** — `GET /v1/templates` (seção
> "Ler o catálogo de templates", mais abaixo) devolve `templates[].motivo` para quando você lê o
> catálogo por outro motivo, ou perdeu este webhook. A diferença é o momento: este avisa **sozinho**,
> assim que a Meta decide; o do catálogo exige você **perguntar**.

**`phone_number_id` não aparece neste evento** — o webhook de template não carrega `metadata` nenhum
(confirmado nos 21 exemplares reais que fundaram esta seção). Se o seu código exige
`phone_number_id` em todo evento, ele quebra aqui.

> ⚠️ **O `id` deste evento inclui o TEMPO, e isso é de propósito — não "simplifique" para
> `template_status:{id}:{event}`.** Para status de mensagem a chave é `status:{wamid}:{status}` e
> basta, porque `sent`/`delivered`/`read` são estados **distintos**: o mesmo par nunca se repete.
> Aqui não: **o mesmo template pode ser `APPROVED` mais de uma vez** — aprovado, editado, volta a
> pendente, aprovado de novo. Sem o tempo na chave, a **segunda aprovação teria o id da primeira** e
> o seu dedup (obrigação 2, mais abaixo) a jogaria fora: você nunca ficaria sabendo. O tempo vem do
> `entry.time` da Meta, porque o `value` deste webhook não tem carimbo próprio.
>
> A chave continua **determinística**: a reentrega do MESMO evento (a Meta reenvia por até 36 h) traz
> o mesmo `entry.time` e portanto o mesmo `id`, e o seu dedup funciona como em qualquer outro evento.

**Exemplo executado** (o parser do gateway sobre um payload de **captura real (parcial)**, 2026-07-26: o
`change` é literal, um dos 21 exemplares guardados em disco desde antes da migração; o nível `entry`
é o envelope-padrão do corpus):

```json
{
  "tipo": "template_status",
  "id": "template_status:1384121316897444:APPROVED:1769000020",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000020,
  "template": {
    "nome": "aguardando_peca_v2",
    "idioma": "pt_BR",
    "categoria": "UTILITY",
    "estado": "APPROVED",
    "motivo": "NONE"
  }
}
```

> 🔴 **Este evento NÃO é o aviso de reclassificação de categoria, e por três dias ele foi tratado
> como se fosse (2026-07-28).** Ele traz `template.categoria` porque a categoria é um
> **atributo** do estado novo — ou seja, você fica sabendo da reclassificação **se, e somente se**, a
> Meta reaprovar o template no mesmo movimento. Quando ela reclassifica um template **já aprovado**,
> sem mexer no estado, este evento **não chega**, e o modo de falha é o pior possível: silêncio, com
> a descoberta chegando na fatura. O evento dedicado é o `template_categoria`, logo abaixo.

**Os demais webhooks de conta:** ver a seção *Webhook de CONTA*, mais abaixo, para a lista completa
do que é modelado e do que chega com `eventos: []` de propósito.

### `template_categoria` (`tipo: "template_categoria"`) — a Meta RECLASSIFICOU a categoria de um template (2026-07-28)

**Este é o evento que avisa que a categoria mudou.** Ele vem do webhook `template_category_update`
da Meta, e existe ao lado do `template_status` acima — os dois falam do mesmo template e **não** são
o mesmo fato. Um template pode ser reclassificado **e** reaprovado, e nesse caso chegam os dois;
deduplicar um pelo outro apaga informação.

**Por que ele importa: reclassificação para `MARKETING` encarece cada envio.** A contestação existe,
mas quem avisa que ela existe é o painel da Meta, **não este evento** — ver o aviso vermelho logo
abaixo da tabela. O que ele dá e o `template_status` não dá:

| Campo | O que é |
|---|---|
| `template_categoria.categoria_anterior` / `categoria_nova` | a **direção**. `UTILITY → MARKETING` encarece; `MARKETING → UTILITY` barateia. O `template_status` diz só "hoje é MARKETING", e sem estado guardado do seu lado os dois casos ficam indistinguíveis |
| `template_categoria.status_do_recurso` | o `category_appeal_status` da Meta — `"ELIGIBLE"` significaria que **dá para contestar**. ⚠️ **Nunca observado em tráfego real** — ver abaixo |
| `template_categoria.categoria_correta` | o `correct_category`: qual categoria a Meta considera correta para este template. ⚠️ **Nunca observado em tráfego real** — ver abaixo |
| `template_categoria.nome` / `idioma` | o par que identifica o template **no envio** (`template` e `idioma` do `POST /v1/messages`) |
| `waba_id` | a **única** chave de roteamento (webhook de conta — não há `phone_number_id`) |
| `timestamp` | o carimbo do lote (`entry.time`) — este webhook **não tem carimbo próprio** |

> 🔴 **Os dois campos do recurso são DOCUMENTADOS pela Meta e NUNCA chegaram em tráfego medido. Não
> construa nada que dependa deles sem confirmar na sua conta.** As linhas da tabela acima descrevem o
> que a documentação da Meta promete; o que se mediu diz outra coisa:
>
> - **35 capturas reais**, de 30/07 a 23/08/2026, numa conta em uso normal (medição do consumidor
>   `consumer-b`, inventário completo de campos por `set(v.keys())`): o conjunto de campos é
>   **fechado em dois formatos**, e nenhum dos dois contém `category_appeal_status` ou
>   `correct_category`. Os campos vistos são `message_template_id`, `message_template_name`,
>   `message_template_language`, `new_category` e — em 34 das 35 — `previous_category`.
> - **Isso inclui templates que passaram por pedido de revisão de categoria**, que é justamente o
>   cenário em que um `category_appeal_status` faria sentido aparecer.
> - As três capturas congeladas em `testdata/corpus/` concordam, e há um teste que fica **vermelho**
>   no dia em que uma captura com esses campos entrar no corpus — a afirmação se corrige sozinha em
>   vez de envelhecer calada.
>
> **O que NÃO se conclui daí:** que a Meta não os manda. Uma conta, um mês, e os campos podem depender
> de um caminho que ninguém percorreu — um recurso formal aberto pelo painel, por exemplo, não é a
> mesma coisa que "pedir revisão de categoria". **O que se afirma é o parágrafo acima, e só ele.**
>
> ➡️ **Consequência prática:** trate `status_do_recurso` e `categoria_correta` como **ausentes por
> padrão**. Quem quiser saber se um rebaixamento é contestável descobre pelo painel da Meta, não por
> este evento. **Se eles aparecerem na sua conta, mande a captura pelo canal** — ela vira fixture no
> mesmo dia e este bloco muda.

> ⚠️ **`categoria_nova` é o único campo deste bloco SEM `omitempty`: ele vem sempre.** Um
> `template_category_update` sem `new_category` (ou sem `message_template_id`) **não vira evento** —
> a chave de dedup colidiria com qualquer outro change vazio do mesmo lote. Você continua recebendo o
> `cru`, e o `parse_error` do envelope acusa. Os demais campos podem faltar individualmente: um campo
> que a Meta mande num formato que ainda não sabemos ler custa **aquele campo**, nunca o evento.

> ⚠️ **`status_do_recurso` é TEXTO, e não vira booleano.** `"ELIGIBLE"` chega como `"ELIGIBLE"`.
> *"Dá para recorrer?"* parece uma pergunta de sim/não, e a Meta responde com um vocabulário que
> ninguém aqui enumerou — traduzir para `true`/`false` obrigaria o gateway a decidir hoje o que fazer
> com um valor que só aparece amanhã. Mesma regra de `template.estado`, `template.categoria` e
> `cobranca.categoria`: **o formato da Meta morre no gateway; o vocabulário de valores dela, não.**

**Exemplo** (o parser do gateway sobre o sample que a Meta publica como exemplo deste webhook):

```json
{
  "tipo": "template_categoria",
  "id": "template_categoria:12345678:MARKETING:UTILITY:1769000070",
  "waba_id": "WABA_TESTE",
  "timestamp": 1769000070,
  "template_categoria": {
    "nome": "my_message_template",
    "idioma": "en-US",
    "categoria_anterior": "MARKETING",
    "categoria_nova": "UTILITY",
    "categoria_correta": "MARKETING",
    "status_do_recurso": "ELIGIBLE"
  }
}
```

> ✅ **O fixture deste evento é CAPTURA DE TRÁFEGO REAL desde 2026-08-28** (três payloads crus
> cedidos pelo consumidor `consumer-b` e congelados em `testdata/corpus/`: o par
> `UTILITY → MARKETING` / `MARKETING → UTILITY` do **mesmo** template, e um terceiro que chegou
> **sem `previous_category`**). Até ali ele era derivado do sample do painel da Meta, e o aviso que
> ficava aqui dizia por quê. ⚠️ **O que a captura mostrou e vale para o seu mapeamento:** nas três
> **não vieram** `correct_category` nem `category_appeal_status` — os dois campos da tabela acima
> saem do que a Meta documenta, e o tráfego observado até agora não os corroborou. São três eventos
> de uma conta: não leia como "a Meta não manda", leia como "não conte com eles estarem lá".

> 🔴 **BLOCO CONTESTADO PELA PRÓPRIA FONTE EM 2026-08-29 — leia isto antes de usar qualquer número
> daqui.** O consumidor `consumer-b`, que produziu esta medição, voltou ao `payload_cru` dos MESMOS
> 35 eventos e relatou pelo canal que **duas afirmações abaixo não se sustentam**:
>
> - **o rebaixamento não atinge template já aprovado e em uso.** Os 15 rebaixamentos aconteceram
>   **de 1 a 13 MINUTOS depois da CRIAÇÃO** do template, sem exceção. O que esta série mede é a
>   categoria com que o template SAI da classificação — não a Meta mudando de ideia depois.
> - **nenhuma volta para `UTILITY` partiu da Meta** — todas são levas de um humano pedindo pelo menu
>   do WhatsApp Manager. Isso derruba especificamente a frase, mais abaixo, que oferece *"14,8 h,
>   14,9 h, 22,1 h e 22,2 h"* como **o número da Meta**: nesta série não há número da Meta.
>
> **O que CONTINUA de pé, e é a razão de o evento existir:** o template passa tempo em `MARKETING`, e
> nesse tempo o envio sai mais caro e sujeito a opt-out. A consequência prática não depende de quem
> iniciou a mudança — só a explicação do mecanismo muda.
>
> **Por que o número novo NÃO entrou no lugar do velho:** este documento já publicou um número desta
> mesma origem que estava errado — ver `ARMADILHAS.md`, *"Número que chega pronto não traz o comando
> que o produziu"* —, e a regra que saiu daquele custo é **número de terceiro entra com o comando que
> o produziu, ou não entra**. O comando foi pedido pelo canal em 2026-08-29 e ainda não chegou. Até
> ele chegar, o bloco abaixo vale como **relatado em 28/08 e contestado em 29/08**, nunca como
> medição corrente.

> 📊 **E NÃO é hipótese: 16 rebaixamentos em 25 dias, com o template ficando de 15 horas a três
> semanas na categoria errada.** Medição do consumidor `consumer-b` na conta dele, relatada em
> 2026-08-28: **35 eventos** `template_categoria` entre **30/07 e 23/08/2026**, contados um a um
> pelos objetos `value` dentro de `entry[].changes[]` (sem truncar a saída, e separando por
> `message_template_id` — contar por nome misturaria versões e daria a conclusão errada). Deles,
> **16 mudanças para `MARKETING`** e 19 para `UTILITY`; nenhuma outra categoria.
>
> **Onze voltaram, e o tempo em `MARKETING` varia em duas ordens de grandeza: de 14,8 h a 512,8 h.**
> 🔴 **Não leia a ponta de cima como lentidão da Meta.** As janelas de 400-500 h terminam todas na
> mesma leva de 21/08 06:48-06:52 — a hora em que um humano pediu revisão de vários templates de uma
> vez. Elas medem **quanto tempo levou até alguém pedir**, não quanto a Meta levou para responder.
> 🔴 **(FRASE DERRUBADA PELA FONTE EM 29/08 — ver o aviso no topo do bloco.)**
> Quem quiser o número da Meta olhe os quatro em que o pedido foi imediato: **14,8 h, 14,9 h, 22,1 h
> e 22,2 h**.
>
> ⚠️ **E cinco NÃO voltaram** — um deles segue em `MARKETING` até hoje. Somando com o parágrafo
> acima: o piso da janela é ~15 h, e **o teto é "até alguém perceber"**. É por isso que consumir este
> evento não é opcional: sem ele, o relógio da janela só começa a correr quando alguém estranha a
> fatura.
>
> **O que isso obriga você a decidir, e é a razão de este evento existir:** durante essa janela, todo
> envio daquela família sai como `MARKETING` — mais caro e sujeito ao opt-out de promoções, num aviso
> que para o seu cliente é transacional. Quem mantém aprovada a versão `UTILITY` imediatamente
> anterior tem para onde cair enquanto o recurso corre; quem só tem a versão no ar, não tem.
> **Guardar a anterior é rollback para ESTA janela** — não é seguro contra a Meta "mudar de ideia
> depois do recurso", que é outra pergunta.
>
> **A ressalva, e ela é sobre o que NÃO foi contado:** "a categoria volta a se mexer depois do
> recurso?" foi verificado explicitamente sobre **quatro** restaurações, com 6 a 7 dias de
> observação, e nenhuma reincidiu. Isso é **evidência a favor**, com `n` pequeno — e não foi
> recontado sobre as onze restaurações da série completa. **Não leia como confirmação.**
>
> 🔁 **Gatilho de revisão, e ele é um pedido a você:** isto é o comportamento da **Meta** medido em
> **uma** conta, num intervalo de 25 dias. Uma segunda conta derruba isso fácil. **Se a sua conta vir
> outra coisa** — outra frequência, rebaixamento sem restauração, reincidência depois do recurso,
> janela de outra ordem de grandeza —, diga: este bloco muda, e quem mediu primeiro pediu
> explicitamente para saber.
>
> ✅ **As três capturas ENTRARAM em 2026-08-28** (T-174) — é o que o aviso verde acima registra, e
> este parágrafo dizia o contrário até então. **Se você tem tráfego real deste webhook, mandá-lo
> pelo canal continua valendo:** três eventos de uma conta congelam a forma, não a variação. A
> segunda conta é que derruba ou confirma o que está escrito aqui.

### `qualidade_do_numero` (`tipo: "qualidade_do_numero"`) — a COTA diária e a qualidade do número (2026-07-28)

Vem do webhook `phone_number_quality_update`. **Este é o único canal pelo qual um rebaixamento de
cota chega antes de doer.** Sem ele, a primeira notícia de que o teto caiu é o envio começando a
falhar por limite — um sintoma que aponta para o lugar errado (todo mundo vai olhar o gateway, o
token e a rede) e que só aparece depois de as mensagens já terem sido recusadas.

| Campo | O que é |
|---|---|
| `qualidade_do_numero.numero_exibido` | o `display_phone_number` — de qual número é o aviso. É **rótulo**, não `phone_number_id`: este webhook não tem `metadata` nenhum |
| `qualidade_do_numero.estado` | o `event` da Meta: `ONBOARDING`, `FLAGGED`, `UNFLAGGED`… — sem tradução |
| `qualidade_do_numero.limite_anterior` / `limite_atual` | `old_limit` → `current_limit`. São os dois que dão a **direção**: `TIER_1K → TIER_50` é rebaixamento, `TIER_NOT_SET → TIER_250` é a conta nascendo. O `limite_atual` sozinho não distingue os dois |
| `qualidade_do_numero.limite_diario_maximo` | o `max_daily_conversations_per_business` |
| `waba_id` | a **única** chave de roteamento |
| `timestamp` | `entry.time` — este webhook não tem carimbo próprio |

> 🔴 **Os limites são TEXTO literal. `"TIER_250"` chega como `"TIER_250"`, e não como `250`.** Isso é
> decisão, não preguiça: converter exige uma tabela de tradução, e ela erra no dia em que a Meta
> inventar um tier novo — errando do pior jeito possível, devolvendo um **número plausível** para um
> valor que ninguém verificou. Se você precisa do número para uma barra de progresso, faça o mapa do
> seu lado e **trate o valor desconhecido como desconhecido**, nunca como zero.

> ⚠️ **Este evento não tem id da Meta**, então a chave é montada com o que ele traz — número, estado
> e a transição de limite, mais o `entry.time`. Um payload em que **nada disso** seja legível não
> vira evento (o `cru` chega assim mesmo, e o `parse_error` acusa); qualquer peça que sobreviva já
> basta para o evento sair.

> **Desde 2026-07-28 este evento também ALIMENTA o `GET /v1/estado`.** O `limite_atual`
> (`current_limit`) é gravado no bloco `numero_na_meta.limite_de_mensagens`, com `fonte: "webhook"`.
> Se você já reage a este evento, não precisa mudar nada — o estado passa a ser só um segundo lugar,
> **consultável a qualquer momento**, onde o mesmo número aparece. `limite_anterior` e
> `limite_diario_maximo` **não** são gravados lá: o estado responde *"em que tier o número está
> AGORA"*, e a direção da mudança já viaja inteira aqui.

### `alerta_de_conta` (`tipo: "alerta_de_conta"`) — a Meta avisando de um problema, com SEVERIDADE (2026-07-28)

Vem do webhook `account_alerts`. **O campo que justifica o tipo existir é `severidade`:** o exemplo
da Meta traz `INFORMATIONAL`, e a existência de um nível chamado "informativo" implica que há níveis
acima dele — são esses que decidem alguma coisa. Sem modelar, um alerta grave e um aviso de rotina
chegavam idênticos: linha crua que ninguém lê.

| Campo | O que é |
|---|---|
| `alerta_de_conta.severidade` | o `alert_severity` — **o campo que decide** |
| `alerta_de_conta.tipo` | o `alert_type` (ex.: `OBA_APPROVED`) — o que aconteceu |
| `alerta_de_conta.estado` | o `alert_status` (ex.: `NONE`) |
| `alerta_de_conta.tipo_da_entidade` / `id_da_entidade` | `entity_type` / `entity_id` — sobre o quê é o alerta. O `id` é **texto**, e a Meta o manda como número |
| `alerta_de_conta.descricao` | o `alert_description`, texto livre em inglês |
| `waba_id` · `timestamp` | como nos demais webhooks de conta |

> 🔴 **O gateway NÃO ordena severidades, e a ausência é deliberada.** Não existe aqui nenhum
> `grave: true` derivado de `severidade`: ordenar o vocabulário de terceiro exige conhecer a lista
> inteira, e ninguém aqui a conhece. Quem decide o que é grave é você, que tem o contexto do negócio
> — o gateway entrega o rótulo intacto para que essa decisão seja possível.

> ⚠️ **Não escreva regra em cima de `descricao`.** É o único campo deste evento que não é vocabulário
> fechado: é frase em inglês escrita pela Meta. Casar texto livre de terceiro é a forma mais rápida
> de construir um alarme que morre no dia em que eles reescrevem a frase. Use `severidade`, `tipo` e
> `estado`.

> ⚠️ **A chave deste evento inclui `severidade` e `estado`, e não só o `tipo`.** O **mesmo**
> `alert_type` pode voltar com severidade diferente (um problema que **escala**) ou com
> `alert_status` diferente (um problema que é **resolvido**). Com uma chave que só levasse o tipo, o
> aviso de escalada seria deduplicado contra o alerta original — e ele é o único dos dois que exige
> ação.

> 🔴 **Os fixtures destes dois eventos são DERIVADOS DA DOCUMENTAÇÃO** (no corpus deste gateway,
> arquivos com `_derivado_da_doc` no nome), pelo mesmo motivo do `template_categoria`: são samples do
> botão *Test* do painel, e não há captura real — webhook de conta é raro por natureza. Testar o seu
> mapeamento só contra eles prova que você concorda com a **documentação** da Meta, não com o que ela
> **manda**.

### Webhook de CONTA (status de template, qualidade do número, alertas) — 2026-07-26

A Meta também manda webhooks que não são sobre uma mensagem nem sobre um destinatário: aprovação ou
rejeição de template (`message_template_status_update`), qualidade do template
(`message_template_quality_update`), mudança de categoria (`template_category_update`) e alertas de
conta (`account_update`, `account_review_update`, `account_alerts`).

**Quatro deles viram evento hoje**, e o resto chega **sem evento nenhum**. A tabela abaixo é a lista
completa, e ela existe para você não tratar "evento que não vira nada" como falha de parse:

| `field` da Meta | Vira evento? | O que você recebe |
|---|---|---|
| `message_template_status_update` | ✅ `tipo: "template_status"` | ver a seção do evento |
| `template_category_update` | ✅ `tipo: "template_categoria"` | ver a seção do evento |
| `phone_number_quality_update` | ✅ `tipo: "qualidade_do_numero"` | ver a seção do evento |
| `account_alerts` | ✅ `tipo: "alerta_de_conta"` | ver a seção do evento |
| `message_template_quality_update` | ❌ **de propósito** | `cru` + `"eventos": []` |
| `message_template_components_update` | ❌ **de propósito** | `cru` + `"eventos": []` |
| `account_update` | ❌ **de propósito** | `cru` + `"eventos": []` |
| `account_review_update` | ❌ **de propósito** | `cru` + `"eventos": []` |
| `security` | ❌ **de propósito** | `cru` + `"eventos": []` |
| `phone_number_name_update` | ❌ **de propósito** | `cru` + `"eventos": []` |
| `calls` | ❌ **ainda não** — ver abaixo | `cru` + `"eventos": []` |
| qualquer `field` que a Meta invente amanhã | ❌ | `cru` + `"eventos": []` |

**"De propósito" quer dizer exatamente isto: ninguém pediu.** O envelope só **cresce** —
acrescentar um campo depois é de graça, tirar depois é quebra de contrato —, então um `tipo` novo
sem consumidor interessado seria vocabulário morto que o gateway passa a dever para sempre.

🔴 **Se algum destes te serve, você NÃO fica sem ele: o `cru` chega inteiro.** O webhook é entregue
normalmente, com os bytes exatos da Meta em base64 — só sem enriquecimento. Parseie o `cru` do seu
lado e você tem o mesmo dado; o `tipo` modelado é conveniência, não acesso. **Deduplique esse lote
pela regra da seção *E quando o lote não tem evento nenhum*** (hash do `cru`), que é a chave certa
justamente para este caso. Se um dia algum destes for modelado, ele nasce com `tipo` próprio, e isso
é **aditivo**: o seu parse do `cru` continua funcionando.

E não é lacuna por esquecimento nem preguiça de parse: o `value` de cada um tem chaves diferentes, e
interpretá-los com o parser de outro produziria um evento **inventado**, que é pior que nenhum.

> ⚠️ **`calls` é o único da lista que é diferente, e vale você saber por quê.** Ele **não** é webhook
> de conta: tem `metadata.phone_number_id`, então passa pela guarda **5a** e numa ligação real o id
> **bate** — ou seja, ele **é entregue a você**, com `"eventos": []`, e o `cru` dele carrega
> **dado pessoal** (`contacts[].profile.name`, `wa_id`) numa linha que ninguém lê. Isso entra na sua
> conta de retenção **hoje**, mesmo sem evento. Modelá-lo depende de uma decisão de negócio que não
> é técnica (o número aceita ligação, e existe alguém para atender?), e por isso ele está fora desta
> rodada.

> 🔴 **A linha do `template_category_update` chegou tarde, e o atraso tem nome.** De 2026-07-26
> a 2026-07-28 o gateway lia a categoria do **vizinho** (`message_template_status_update`)
> e chamava isso de aviso de reclassificação. Se você construiu alarme de custo em cima de
> `template.categoria`, ele continua valendo — mas ele **só dispara quando a Meta reaprova o template
> junto**. O sinal completo é o `template_categoria`.

> 🔴 **Um webhook de conta não modelado chega até você SEM evento nenhum, e isso é NORMAL — nunca
> falha de parse (2026-07-28).** O parser não falhou: ele simplesmente não achou `messages`
> nem `statuses` para enriquecer. Grave o `cru` (obrigação 1) e responda `200`. **Não alarme, não
> trate como erro, não devolva `5xx`** — um `5xx` aqui faria a Meta reenviar o mesmo payload por 36 h
> para um lote que não tinha nada a processar.
>
> ✅ **O campo vem como `[]` — array vazio, nunca `null`.** O corpo é literalmente:
>
> ```json
> {"instancia":"…","recebido_em":"…","cru":"…","eventos":[],"parse_error":""}
> ```
>
> A normalização é feita num lugar só, na montagem do envelope, e é travada por um teste que afirma
> sobre os **bytes do fio** — não sobre a estrutura em memória, que é o que deixou o defeito abaixo
> passar despercebido por cinco dias.
>
> ⚠️ **DE 2026-07-23 A 2026-07-28 ESTE CAMPO ERA `null` NO FIO, e a defesa que você escreveu
> por causa disso continua valendo.** O parser produzia uma lista **nula** quando não havia evento
> nenhum, o envelope a repassava sem normalizar, e serializar uma lista nula em Go produz `null`.
> Quem seguia este contrato ao pé da letra
> escrevia `for ev in envelope["eventos"]` e levava `TypeError: 'NoneType' object is not iterable`
> em Python; um `if eventos == []` **nunca** casava.
>
> **Não desfaça essa defesa.** `for ev in (envelope.get("eventos") or [])` funciona igual com `[]`,
> continua sendo a forma recomendada, e é o que protege você de qualquer futuro em que o campo volte
> a faltar. A direção da mudança foi escolhida justamente por isso: quem tolerava `null` continua
> funcionando, e quem quebrava passou a funcionar.
>
> ⚠️ **Se for escrever a defesa hoje, escreva `or []`, NUNCA `get("eventos", [])`** — e a diferença
> não é estilo. Em Python o default do `get` só vale quando a **chave não existe**; ela sempre
> existe aqui, então um `null` (do histórico, de um envelope antigo que você tenha guardado, ou de
> qualquer outra fonte) passa reto pelo default e você recebe `None` de volta.
> `envelope.get("eventos", [])` **parece** a versão limpa da linha acima e é a "simplificação" que
> qualquer revisor sugeriria. *Levantado por um consumidor em 2026-07-28, depois de conferir o
> próprio código: ele não quebrou, mas por acidente, e a armadilha que ele nomeou é esta — não é só
> o `for` estourando, é o **conserto natural** que traz o bug de volta.* O equivalente em JS (`??`,
> não `||` — embora aqui os dois funcionem) e em Go (lista nula já itera zero vezes) não tem essa
> pegadinha; ela é de Python, e por isso está escrita com o nome exato do erro.
>
> **`parse_error` não é `null` nem ausente: ele é a string vazia (`""`)** quando não houve erro, e
> **isso nunca mudou nem vai mudar**. O campo não é omitido
> e por isso vem **sempre**. Teste por conteúdo (`if parse_error:`), nunca por presença da chave.
> *Por que ele não entrou no conserto de `eventos`: o vazio dele já é **falso** em
> toda linguagem, e ninguém itera sobre uma string de erro. `""` não quebra nada; `null` num campo
> que se ITERA, sim. Essa é a regra.*
>
> **Como distinguir os dois casos, que é a única coisa que você precisa saber aqui:**
>
> | O que chegou | `eventos` | `parse_error` | O que significa |
> |---|---|---|---|
> | webhook de conta não modelado | `[]` | `""` | normal — grave o `cru` e siga |
> | payload que o gateway não conseguiu interpretar | `[]` (ou parcial) | **texto do erro** | o `cru` está aí e é a fonte: grave-o, e trate o lote pela chave de dedup do `cru` |
>
> 🔴 **Por que este aviso existe, e por que ele provavelmente vale para VOCÊ também:** ao salvar uma
> Callback URL nova, o painel da Meta inscreve o App num **conjunto padrão de campos de uma vez** —
> dez, na medição de 2026-07-28 —, e o gateway modela só parte deles (a tabela acima diz **quais**,
> e é ela que vale, não uma contagem). Ou seja, **lote sem evento não é
> exceção rara: é tráfego de rotina desde o primeiro dia da sua instância**, e "evento que não vira
> nada" é fácil de confundir com "falha de parse". Confira, no painel do seu App, a quais campos ele
> está inscrito — é lá que se decide quanto desse tráfego você vai receber.

**A Meta não deixa você ter uma URL de webhook por instância para estes.** Confirmado em
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/override/ (lido em
2026-07-26): mensagem e status suportam override de URL por número/WABA; os campos de template e de
conta acima **não** — chegam sempre na URL principal do App na Meta, nunca na sua `callback_url` por
instância.

**Isso significa que o gateway tem de decidir, sozinho, se um webhook de conta que chegou é ou não seu**
— e a chave que ele usa para decidir é o `waba_id` (`entry[].id` no payload da Meta, confirmado como
*"WhatsApp Business Account ID"* nas páginas oficiais citadas acima e em
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/status/,
mesma data). Se o `waba_id` do webhook bater com o da sua instância, você recebe o `cru` normalmente,
pelo mesmo `POST` na sua `callback_url`. **Se não bater, você não recebe nada** — o gateway descarta e
responde `200` à Meta, sem tentar adivinhar para qual outra instância aquilo pertence.

> ⚠️ **Até 2026-07-26, um webhook de conta que chegasse na URL principal era entregue ao consumidor
> configurado nela mesmo que pertencesse a outra WABA — e não era só roteamento errado.** A guarda de
> isolamento entre inquilinos só comparava `phone_number_id`, e webhook de conta não tem esse campo. Se
> você tinha um `if` que confere `envelope["instancia"]` contra o slug configurado (comportamento
> recomendado, ver *A instância errada*, mais abaixo), ele **passava** — porque é o próprio gateway quem
> põe o slug do caminho no envelope — e um webhook de conta de outro inquilino podia acabar gravado no
> seu banco como se fosse seu. Isso está fechado agora: um webhook de conta só chega a você se a `waba_id`
> for mesmo a da sua instância.

**Ela deixou de ser só teoria em 2026-07-26:** uma segunda instância de teste foi criada no
gateway de produção, exercitada com tráfego assinado e removida no mesmo dia. Uma assinatura válida
para uma instância, mandada no caminho da outra, levou `403`; e o alarme de `phone_number_id` alheio,
que nunca havia disparado, disparou quando provocado. Naquele exercício a origem do tráfego foi o
próprio gateway sendo testado, não a Meta.

> **Uma frase foi RETIRADA daqui em 2026-07-28 em vez de atualizada, e o motivo vale mais que
> ela:** este parágrafo afirmava *"hoje há uma única instância cadastrada neste gateway"*. Era
> verdade quando foi escrito e envelheceu **em silêncio** — contagem de produção muda sem que o
> contrato mude junto, e um número desatualizado num doc é pior que número nenhum, porque o leitor
> confia. **Este documento não responde quantas instâncias existem, e nunca vai responder.** O que
> ele promete é a **garantia**, que não depende da contagem.

### O que separa você do outro inquilino é a ASSINATURA, não a guarda de id (2026-07-28)

**A doutrina de isolamento deste projeto vinha descrevendo as guardas de `phone_number_id` e
`waba_id` (as duas seções acima) como o que separa um inquilino do outro. Não é.** A correção está
aqui porque um consumidor que leia as seções acima e pare por ali conclui que "contador de descarte
em zero" significa "nenhum tráfego alheio passou por perto" — e isso é falso.

São **duas camadas**, com propósitos, respostas HTTP e alcances diferentes:

| | **A fronteira** | **A conferência de endereçamento** |
|---|---|---|
| O que é | a **assinatura HMAC da Meta, conferida com o `app_secret` DAQUELA instância** | as guardas 5a (`phone_number_id`) e 5b (`waba_id`) |
| Onde | no passo 3 do processamento do webhook | nos passos 5a e 5b, depois do parse |
| O que responde | **`403`, e o processamento para ali** — nada chega ao passo 4 (parse) nem ao 5 | **`200`, descartando o lote** |
| Do que protege | de **outro App** — ou seja, de outro inquilino, e de quem descobrir a sua URL | de **erro de configuração seu**: Callback URL apontada para o caminho errado, override de webhook ausente, WABA com mais de um número |
| Prova | exercitada com tráfego real em 2026-07-26: os **mesmos bytes** com a **mesma assinatura**, mandados no caminho da outra instância, levaram `403` | provocada de propósito na mesma tarefa: `200` + `ALARME` + contador |

**As guardas 5a/5b existem para pegar erro de configuração, não invasor.** Um invasor não erra o
`phone_number_id` — ele nem chega ao passo 5, porque não tem o `app_secret` para assinar.

> 🔴 **O corolário, que ninguém deduz sozinho a partir das seções acima: as guardas ficam MUDAS
> quando dois Apps compartilham o mesmo número e a mesma WABA.** Nesse cenário o `phone_number_id`
> do corpo **bate** com o cadastrado e o `waba_id` **bate** também — as duas guardas veem tudo certo,
> nenhum `ALARME` sai, e **nenhum contador se move** (`numero_descartado` e `conta_descartada`
> seguem em zero). **Silêncio das guardas não é prova de que não há convivência**; é prova de que o
> endereçamento está coerente, que é outra pergunta. Quem tratar um zero ali como "ninguém mais está
> plugado neste número" vai concluir errado — e este projeto concluiu errado exatamente assim, em
> 2026-07-28, antes de perguntar a quem era dono do número.
>
> **O que continua garantido nesse cenário, e é o que importa:** o outro App tem **outro
> `app_secret`**, então o webhook dele não fecha a assinatura da sua instância e leva `403` no passo
> 3. A fronteira continua de pé — só não é a guarda de id que a sustenta. **Por isso não há
> contramedida nova aqui**: uma guarda a mais para "ids iguais" seria complexidade sem ameaça, já
> que a camada que decide é outra.

**A frase acima tem uma condição, e escondê-la seria trocar um doc errado por outro:** "a assinatura
é a fronteira" vale porque **cada consumidor usa um App próprio** na Meta (decisão de projeto,
2026-07-26), e App próprio significa
`app_secret` próprio. **Duas instâncias no MESMO App teriam o mesmo `app_secret`** — e nada no
gateway impede cadastrar o mesmo valor duas vezes
— e nessa configuração a assinatura **deixa de distinguir** as duas: o webhook de uma fecha a conta
da outra, e quem separa passa a ser exatamente 5a/5b. **É para essa configuração que as duas guardas
existem**, e é por isso que elas não são cerimônia. O que muda com esta seção é o **nome**: hoje, com
um App por consumidor, elas conferem endereçamento; no dia em que dois números dividirem um App, elas
viram a única separação — e aí o zero delas passa a significar algo bem diferente.

### A sua `callback_url` tem de ser `https://`

O gateway **recusa criar** uma instância com `callback_url` em `http://`
na criação. Não é preferência: o `POST` acima leva o **corpo
cru** da mensagem — o que o cliente escreveu, o telefone dele, o nome dele. Em `http://` isso
atravessa a rede legível para quem estiver no caminho, e o `X-Zapgw-Signature` não ajuda:
**assinatura prova integridade, não confidencialidade.**

Duas exceções, e só duas:

- **`callback_url` vazia é válida** — é a instância só de saída, que envia e não recebe. Ausência de
  entrega não é entrega em claro.
- **`http://127.0.0.1` e `http://localhost`** (com qualquer porta e caminho), para desenvolvimento na
  própria máquina. A checagem é sobre o **host** que a URL resolve, então
  `http://127.0.0.1.exemplo.com` e `http://127.0.0.1@exemplo.com` são recusadas — elas começam com o
  texto permitido e entregam para fora.

### E o seu certificado é verificado — não há como desligar isso

Exigir `https://` sem verificar o certificado seria teatro: a URL continuaria dizendo `https` e não
haveria garantia nenhuma por trás. Então a verificação na entrega é **estrita** e **não existe opção
de desligá-la** — nem flag, nem variável de ambiente, nem campo de configuração, nem "só em
desenvolvimento". Isso é regra de projeto, não
preferência de quem implementou: desligar a verificação não gera erro nenhum, só remove uma proteção,
e por isso a opção fica ligada para sempre no dia em que existir.

Na prática, o que isso exige de você:

- **Certificado válido, dentro da validade, para o hostname da sua `callback_url`.** Autoassinado sem
  mais nada não passa.
- **Se você usa CA própria** (consumidor interno, sem certificado de CA pública), essa é a saída, e
  ela existe justamente para a escotilha não precisar existir: mande a **CA** para quem opera o
  gateway, e ela é cadastrada **naquela instância** (`--bundle-ca`, arquivo PEM, guardado cifrado em
  repouso). A CA cadastrada por uma instância **não** vale para nenhuma outra. Continua sendo
  verificação: cadeia, validade e hostname seguem conferidos.
- **Certificado vencido é o caso mais caro, e você é quem enxerga primeiro.** Do lado do gateway a
  entrega falha, ele responde `504` à Meta (mantendo a janela de reenvio aberta, que é o que dá tempo
  de alguém consertar) e grava uma linha `ALARME` dizendo que é certificado e o que fazer
  no log do serviço. Mas **nenhum reenvio conserta um certificado**: quando a Meta
  desistir, as mensagens daquele período se perdem em definitivo. Renove antes.

O **slug** da instância também é validado na criação, e é **imutável** depois: minúsculas `a-z`,
dígitos e hífen, sem hífen nas pontas, de 3 a 40 caracteres. Ele vira `/v1/inbound/{slug}`, e um
caractere fora dessa forma produz uma URL que a Meta aceita colar e nunca consegue verificar — com a
mensagem de erro **dela** apontando para o lugar errado.

---

## Enviar uma mensagem

**`POST https://zapgw.exemplo.com.br:8443/v1/messages`** · `Authorization: Bearer <seu token>`
· `Idempotency-Key: <chave sua>`

**A porta não é detalhe — é onde o envio existe, e ela só responde na LAN** (a limitação anunciada no
começo deste documento). O endereço público do gateway serve **apenas** o `/v1/inbound`; o envio mora
num entrypoint interno, e da internet essa porta **não existe** — conferido em 2026-07-28 a partir de
12 pontos fora da rede, todos com timeout, tendo o caminho público como controle positivo. Chamar
`/v1/messages` no endereço público (porta 443) devolve **`404`**, de propósito.

> ⚠️ **Esse `404` é o sintoma mais enganoso deste documento.** Ele é indistinguível de "caminho
> errado" ou "esta versão do gateway não tem essa rota", e manda investigar deploy e código quando o
> problema é **em qual endereço você bateu**. Se você levou `404` numa rota que existe nesta página,
> confira a base da URL e a porta **antes** de qualquer outra coisa.

O certificado é válido para o hostname que você recebeu — **não desabilite a verificação de TLS** no
seu cliente HTTP. O que trafega aí é o seu token, e foi exatamente para ele não andar em claro pela
rede que esse caminho é `https`.

O `Idempotency-Key` é **obrigatório**. Sem ele o pedido é recusado com `400` — não porque somos
rígidos, mas porque sem ele um retry seu vira **duas mensagens no celular do seu cliente**, e nós
não temos como distinguir.

Escolha uma chave que identifique a *intenção*, não a tentativa: o id da sua linha de fila serve; um
UUID novo a cada retry não serve para nada.

> ⚠️ **A chave vai para o log do gateway** quando algo dá errado com ela (é o único jeito de alguém
> conseguir destravá-la). Não coloque dado pessoal na chave — nada de telefone, CPF ou e-mail. Um id
> interno seu não é dado pessoal para quem lê o log; um telefone é.

> 🔴 **A chave viaja num CABEÇALHO HTTP: qualquer caractere não-ASCII nela morre no SEU cliente, antes
> de sair.** Emoji, acento, `ç` — o cliente HTTP levanta exceção e **nenhum pedido chega ao gateway**.
> Não procure no nosso journal: não há nada lá. Do lado de quem opera, o sintoma é `500` no seu
> sistema e silêncio absoluto no nosso.
>
> *Levantado por um consumidor em 2026-07-28, com o erro exato:*
> `UnicodeEncodeError('ascii', 'reacao:<id>:❤', 'ordinal not in range(128)')`. *Ele tinha posto o
> emoji na chave de propósito — para distinguir 👍 de ❤️ como duas intenções, o que está certo (ver
> `reacao`, abaixo). O defeito era só o transporte.*
>
> **Conserte ESCAPANDO, não sanitizando** — e a diferença é a armadilha: `quote`/percent-encoding é
> **injetivo** (👍 e ❤️ continuam gerando chaves diferentes); *remover* os caracteres não-ASCII
> **colapsaria as duas na mesma chave**, e aí o segundo pedido levaria `422` ou, pior, a intenção
> ficaria congelada na primeira. Trocar um erro barulhento por um bug silencioso é o pior negócio
> disponível. **Escape num ponto só**, na borda que monta o cabeçalho.
>
> *Vale para toda chave que carregue texto de humano: título de alerta em pt-BR tem acento, e o mesmo
> tiro fica armado ali.*

**Uma chave serve para UM pedido.** A mesma chave com um corpo diferente é recusada com `422`, e isso
não é rigor: o contrato recomenda usar o id da sua entidade como chave, e a mesma entidade costuma
mandar várias mensagens (lembrete, cobrança, desculpa). Sem essa recusa, a segunda receberia `200`
com o `wa_message_id` da **primeira**, você gravaria "enviado", e a mensagem **nunca sairia**. O
gateway compara um hash do pedido já normalizado — espaço a mais ou a menos não conta como pedido
diferente.

```jsonc
{ "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "texto" | "template" | "botoes" | "cta_url" | "lista" | "midia" | "reacao" | "localizacao" | "contatos" | "pedir_localizacao" | "flow",
  "responder_a": "wamid…",   // opcional; PROIBIDO em template

  "texto": "…",                                   // texto, botoes, cta_url, lista, pedir_localizacao, flow
  "template": "lembrete", "idioma": "pt_BR",      // template
  "variaveis": ["Maria", "19h"],                  // template, opcional (o corpo do template)
  "cabecalho": {"tipo": "documento", "media_id": "…", "nome_arquivo": "recibo.pdf"}, // template, opcional
  "botoes_template": [{"indice": 0, "tipo": "url", "texto": "…"}, // template, opcional
                       {"indice": 1, "tipo": "resposta_rapida", "payload": "…"}],
  "botoes": [{"id":"SIM","titulo":"Sim"}],        // botoes (mensagem interativa — NAO e template) — titulo: maximo 20 caracteres, lista: maximo 3 itens
  "botao_titulo": "Abrir", "botao_url": "https://…", // cta_url — botao_titulo: maximo 20 caracteres, botao_url obrigatório só em cta_url
  "secoes": [{"titulo": "…", "itens": [{"id":"…","titulo":"…","descricao":"…"}]}], // lista, obrigatório — botao_titulo (reusado de cta_url) é o rótulo do botão que abre a lista; ver a seção própria para os tetos
  "fluxo": {"id": "…", "token": "…", "acao": "navigate", "tela": "…"}, // flow, obrigatório — id OU nome (nunca os dois), token obrigatório; botao_titulo (reusado, teto próprio) é o flow_cta; ver a seção própria
  "cabecalho_texto": "…", "rodape": "…",          // botoes, cta_url, lista — opcional

  "media_id": "…",                                // midia, obrigatório (de POST /v1/media)
  "categoria": "imagem"|"video"|"audio"|"documento"|"sticker", // midia, obrigatório — PTT tambem e "audio"
  "legenda": "…",          // midia: só imagem, video e documento
  "nome_arquivo": "nota.pdf", // midia: só documento

  "reacao": {"alvo": "wamid…", "emoji": "👍"},     // reacao, os dois obrigatórios — ver a seção própria
  "localizacao": {"latitude": 37.44, "longitude": -122.16, "nome": "…", "endereco": "…"}, // localizacao; nome/endereco opcionais
  "contatos": [{"name": {"formatted_name": "João Vendedor"}, "phones": [{"phone": "5511999990000"}]}] // contatos, obrigatório — só name.formatted_name é exigido; o interior do cartão usa os nomes de campo da própria Cloud API (name, phones[].phone, org.company…), em inglês, porque são ~25 campos aninhados do vCard e uma tabela de tradução divergiria no primeiro campo novo da Meta
}
```

> ⚠️ **`botoes` e `botoes_template` são DUAS COISAS DIFERENTES, apesar do nome parecido.**
> `botoes` é o corpo inteiro de uma mensagem interativa comum (`"tipo": "botoes"`, sem template),
> com a forma `{"id": "…", "titulo": "…"}`. `botoes_template` é um parâmetro de **template**
> (`"tipo": "template"`), com a forma `{"indice": …, "tipo": "url"|"resposta_rapida", …}` — ver a
> seção *Template: header e botão de URL*, abaixo. Confundir os dois é `400`, erro nomeado apontando
> o campo certo: o gateway não descarta o campo errado em silêncio, mas se você não ler a mensagem
> de erro pode achar que "botoes" também vale para template. **Não vale.**

> ⚠️ **`instancia` é o NÚMERO, não é você.** É o **slug** do número de WhatsApp que envia — o item 1
> do seu pacote de entrega. Quem *você* é já vem no `Authorization: Bearer`; não existe campo para
> isso no corpo, e pôr o **seu** nome em `instancia` é o erro mais fácil de cometer aqui, porque os
> dois são nomes de coisas nossas e parecem intercambiáveis.
>
> **O sintoma, se você errar, não aponta para o erro.** No envio é `404 instancia desconhecida` ou
> `403 instancia nao autorizada para este consumidor` — esse ainda é legível. Na **entrega** é pior:
> um consumidor que confira `envelope["instancia"]` contra o valor configurado (comportamento
> recomendado, ver *A instância errada*) responde `503` a toda entrega, a Meta reenfileira por 36 h,
> e o `503` teimoso **parece problema de assinatura**. Três exemplos deste próprio documento já
> traziam, em `instancia`, o nome de um **consumidor** em vez do slug — corrigidos em 2026-07-26,
> depois de alguém ler o exemplo antes de usá-lo. É por isso que a seção de convenções, lá em cima,
> diz que **todo** valor de exemplo daqui é fictício.

Sucesso devolve `200` e `{"wa_message_id": "wamid…"}`. **Repetir a mesma chave devolve o mesmo id,
sem enviar de novo — mas só dentro da janela de retenção do registro de idempotência.**

> 🔴 **O `wa_message_id` NÃO É OPACO: ele carrega o telefone do destinatário dentro dele, em base64.**
> Isso não é escolha nossa — é o formato da Meta —, mas é obrigação nossa avisar, porque somos nós que
> pedimos que você **guarde** esse id para deduplicar.
>
> ```
> $ echo "<a parte depois de 'wamid.'>" | base64 -d | strings
> ```
>
> O número sai na **forma canônica do WhatsApp**, que para o Brasil é **sem o nono dígito**. Essa é a
> parte que engana: uma busca pelo telefone como um humano o escreve — com o `9` — **passa limpo por
> cima** e devolve "não achei nada", sobre uma busca incapaz de achar.
>
> **As três consequências práticas para você:**
>
> 1. **`wamid` não vira fixture.** Se você grava um caso de teste a partir de tráfego real, mascarar
>    `recipient_id` e `de_cru` **não basta** — o telefone continua inteiro dentro do `wamid` do mesmo
>    arquivo, e o resultado *parece* mascarado. Para fixture, gere um `wamid` sintético.
> 2. **Sua varredura por telefone precisa DECODIFICAR.** Um `grep` pelo número não encontra o que está
>    em base64. A nossa decodifica todo `wamid.<payload>` da árvore antes de comparar, justamente por
>    isso.
> 3. **O mesmo vale para o `cru` do envelope de entrada** (a seção de recebimento): ele é o corpo
>    exato que a Meta mandou, então tem telefone em texto **e** dentro dos `wamid` que vierem nele.
>
> ⚠️ **Isto não é teoria.** Em 2026-08-30 um consumidor mascarou o telefone no corpo de um pedido e
> colou o `wa_message_id` inteiro ao lado, no mesmo relato; ao procurar com a decodificação, achou
> **outro `wamid`, já commitado no repositório dele havia semanas**, com o mesmo número dentro.
> Repositório privado, prejuízo zero — e a correção, se fosse público, não seria `git rm`.
>
> *Este aviso viveu dois meses só na documentação interna do gateway, que você não lê. Ele está aqui
> agora porque a armadilha é do formato que **nós** distribuímos.*

> ⚠️ **O que o `200` significa, e o que ele NÃO significa.** `200` + `wa_message_id` quer dizer **"a
> Meta aceitou o pedido"** — nunca **"a mensagem chegou"**, nem **"vai chegar"**. Isso sempre foi
> verdade (a confirmação de entrega de fato é o evento `status` do webhook — ver a seção de
> recebimento), mas agora tem um caso concreto em que a distância entre as duas coisas fica visível
> na própria resposta do envio:
>
> Para mensagens com `"tipo": "template"` cujo template esteja sob **pacing** (mecanismo da Meta que
> segura parte de um envio em massa para colher feedback antes de liberar o resto — decisão da Meta
> sobre a saúde do seu template, não algo que você escolhe no pedido), a resposta pode trazer um
> campo extra:
> `{"wa_message_id": "wamid…", "message_status": "held_for_quality_assessment"}`. Quando isso
> acontece, o `200` **não garante entrega**: a Meta pode liberar a mensagem depois (feedback
> positivo) ou **descartá-la sem nunca entregar** (feedback negativo) — as duas coisas continuam
> sendo `200` para o gateway, porque o pedido foi mesmo aceito.
>
> **`message_status` só aparece no corpo quando o valor é diferente de `"accepted"`.** Isso é
> deliberado, não economia de bytes: `"accepted"` e o campo **ausente** (o caso de hoje, para todo
> envio que não seja template sob pacing — texto, mídia, interativo, reação, localização, e até
> template fora de pacing) significam exatamente a mesma coisa do ponto de vista de quem só lê
> `wa_message_id`, e por isso os dois produzem o **mesmo corpo de sempre**. Só um valor que muda o
> que o `200` promete — hoje, `held_for_quality_assessment` ou `paused` — aparece.
>
> **Exemplo do formato, com os dois valores que a Meta documenta para esse caso** — o par chave/valor
> é exatamente o que sai (espaçamento aqui é só para leitura), e os dois valores estão presos por
> teste:
>
> ```json
> {"wa_message_id": "wamid.RETIDO", "message_status": "held_for_quality_assessment"}
> ```
> ```json
> {"wa_message_id": "wamid.RETIDO", "message_status": "paused"}
> ```
>
> **O gateway não traduz esse valor, nem para português, nem para as classes de erro do envio**
> (`retentavel`/`permanente`/`config` — ver a seção de erros). Ele repassa o que a Meta mandou, cru.
> Se você receber um `message_status` que não seja `held_for_quality_assessment` nem `paused`, trate
> como "a Meta está dizendo algo que ainda não documentamos" — não como sucesso silencioso.
>
> **Fonte, lida em 2026-07-26:**
> `developers.facebook.com/documentation/business-messaging/whatsapp/messages/send-messages` — o
> campo "é incluído na resposta só quando o envio é de um template message que usa um template sob
> pacing" (tradução nossa); e
> `developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-pacing` —
> sobre o desfecho de `held_for_quality_assessment`: *"If the feedback is positive... the held
> messages will be released and sent normally"*, mas *"If the feedback is negative... Each held
> message will be dropped"*. As duas páginas oficiais **discordam** sobre se `paused` é um valor do
> `message_status` da mensagem ou do `status` do template — o gateway não decide qual das duas está
> certa, só repassa o valor cru.

O registro é purgado depois de um TTL (default **72h**, configurável por
`ZAPGW_TTL_IDEMPOTENCIA_HORAS` do lado de quem opera). Passada essa janela, a mesma chave não é
mais reconhecida: o gateway a trata como um envio novo e **envia a mensagem de novo** — exatamente o
que esta funcionalidade existe para impedir. A garantia não é "para sempre"; é "enquanto o registro
existir". Se o seu processo de retry pode ficar parado por mais que o TTL (fila travada, deploy
demorado, incidente), não confie na chave sozinha depois disso — confira antes se a mensagem já saiu
por outro meio (ex.: consultando se o `wa_message_id` já foi recebido no seu lado).

### Cabeçalho e rodapé nas mensagens interativas (`botoes` e `cta_url`) (2026-08-20)

Uma mensagem interativa aceita **cabeçalho e rodapé em texto livre**, e eles não passam por aprovação
nenhuma: valem dentro da janela de 24 h, como o resto da mensagem.

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "botoes",
  "cabecalho_texto": "Racer Autopeças",        // opcional, máximo 60
  "texto": "Confirma o orçamento #4471?",
  "rodape": "Responda até as 18h",             // opcional, máximo 60
  "botoes": [{"id": "SIM", "titulo": "Confirmar"}, {"id": "NAO", "titulo": "Cancelar"}] }
```

Os dois campos valem **só** em `tipo: "botoes"` e `tipo: "cta_url"`. Mandados em qualquer outro tipo,
são `400` nomeando o campo — nunca descartados em silêncio.

**O teto é 60 caracteres em cada um, contados em CARACTERES e não em bytes** — acento e emoji custam
um, como você espera. *(Fonte: documentação da Meta,
`developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-list-messages`, lida em
2026-08-20. Se ele apertar alguém na prática, diga: no catálogo do maior consumidor de hoje, medido
em 2026-08-20, o maior rodapé em uso tem 33 caracteres e o maior cabeçalho 35 — 60 sobra com folga,
mas isso é a medição de um catálogo, não uma garantia sobre o seu.)*

> ℹ️ **`cabecalho_texto` é só TEXTO.** A Cloud API também aceita cabeçalho de **mídia** em mensagem
> interativa (imagem, vídeo, documento); **este gateway não monta isso hoje**, e não há campo para
> pedi-lo. Não é recusa de projeto — é ausência de caso de uso. Se você tem um, peça: o nome do campo
> foi escolhido com o sufixo `_texto` justamente para caber um `cabecalho_midia` ao lado sem renomear
> nada do que já existe.

#### 🔴 Isto NÃO existe em template, e a diferença não é do gateway — é da Meta

A pergunta aparece sempre, e a resposta economiza uma tarde: **em template, cabeçalho e rodapé são
fixos no cadastro.**

| | no template (aprovado pela Meta) | na mensagem interativa (`botoes`, `cta_url`) |
|---|---|---|
| **cabeçalho** | texto fixo com **no máximo 1 variável**, ou mídia | texto livre, seu, a cada envio |
| **rodapé** | texto fixo, **sem variável nenhuma** | texto livre, seu, a cada envio |

*(Fonte: `developers.facebook.com/docs/whatsapp/business-management-api/message-templates/components`,
lida em 2026-08-20 — "Text headers support 1 parameter"; o rodapé é descrito como componente de texto
e não admite parâmetro.)*

Consequência prática: **não existe como variar o rodapé de um template no envio.** Se o texto precisa
mudar por mensagem, ou ele vira mensagem interativa (dentro da janela de 24 h), ou ele entra no
**corpo** do template como variável. Mandar `rodape` ou `cabecalho_texto` num `tipo: "template"` é
`400`, com a mensagem apontando para `cabecalho` — que é o header **de template**, outro campo e outra
forma.

> ⚠️ **E o gateway não vai colar o texto do rodapé no fim do corpo para simular um rodapé.** Ficaria
> parecido na tela, e seria conteúdo que **nós** escrevemos dentro da **sua** mensagem, sem você ver.
> Se você quer aquela frase no corpo, escreva-a você — aí ela é sua, revisada, e você controla.

#### `botao_titulo` do `cta_url`: máximo 20 caracteres, e este número tem outra origem

O rótulo do botão de link é recusado acima de **20 caracteres**, com `400` nomeando `botao_titulo`.

**Este teto não vem de documentação, e o documento não vai fingir que vem.** A referência oficial da
Cloud API (`developers.facebook.com/docs/whatsapp/cloud-api/reference/messages`, lida em 2026-08-20)
**não declara limite nenhum** para esse campo. O 20 é **medição em aparelho**, feita pelo consumidor
`consumer-b` em 18/08/2026 por bisseção: 17 passou, 21 falhou, 19 e 20 passaram.

**Por que o gateway recusa na entrada em vez de deixar a Meta recusar:** porque o que o gateway
REPASSAVA da recusa dela chegava anônimo. Ela responde `(#131009) Parameter value is not valid`, e até
a T-141 o gateway só lia `message` e `code` do corpo de erro — então era **isso**, sem o parâmetro, que
chegava até você. O custo disso já foi pago — uma mensagem de cliente saiu **sem o botão**, e descobrir
o motivo exigiu bisseção manual num número de teste.

🔴 **Correção: "a Meta não diz qual parâmetro" nunca foi conferido — o que faltava era a nossa
leitura.** Há evidência de que ela nomeia o campo e o teto em `error_data.details` (um registro de
18/07/2026, achado pelo consumidor por outro transporte, veio com "Button title length invalid. Min
length: 1, Max length: 20"). Desde a T-141 o gateway lê **só** essa chave e repassa em `detalhe_meta`
(ver a seção *"O que cada erro quer dizer"*, abaixo) — mas se a Meta ainda manda esse detalhe pelo
caminho de hoje é questão **aberta** até medição contra produção; não trate `detalhe_meta` como
garantido em toda recusa. Por isso o teto continua na entrada: uma guarda que valida antes de chamar a
Meta não depende de ela mandar detalhe nenhum.

> ⚠️ **Recusar é tudo o que o gateway faz aqui.** Ele não encurta o rótulo, não troca para template e
> não escolhe outro caminho por você — isso é orquestração, e o contexto para decidir está do seu
> lado, não do nosso. Se o seu rótulo pode passar de 20, confira **antes** de pedir.

> 🔎 **O vizinho tem o mesmo teto, e desta vez a medição é NOSSA.** O `titulo` de cada item de
> `botoes` (o botão de resposta rápida) também é recusado acima de **20 caracteres**, com `400`
> nomeando `botoes[N].titulo` — o índice do botão errado, não só o campo. Medido em 2026-08-20,
> contra a produção real da Meta (instância `tenant-one`, mensagens enviadas de verdade): 20
> caracteres passa, 21 falha com o mesmo `(#131009)` anônimo, e um rótulo de 20 caracteres
> **acentuados** (40 bytes) também passa — a contagem é por caractere, não por byte. Como o teto,
> ele não é documentado pela Meta: se ela mudar o valor, só uma nova medição em aparelho corrige.

#### `botoes[]`: máximo 3 itens (2026-08-20)

A lista `botoes` (mensagem interativa `tipo: "botoes"`, botão de resposta rápida) aceita **de 1 a 3**
itens. Com 4 ou mais, é `400` nomeando `botoes` com quantos vieram e o máximo.

**Medido em produção, contra a Meta de verdade, em 2026-08-20**, com a `v0.52.0` já no ar — quatro
botões enviados de verdade pela instância `tenant-one`:

```
HTTP 400
codigo_meta  131009
mensagem     (#131009) Parameter value is not valid
detalhe_meta Invalid buttons count. Min allowed buttons: 1, Max allowed buttons: 3
```

Este `detalhe_meta` é o repasse da T-141 (ver *"O que cada erro quer dizer"*, abaixo) funcionando pela
primeira vez contra a Meta de verdade — foi ele que revelou este limite. Diferente do teto de 20
caracteres do vizinho acima (achado por bisseção manual), este número **veio lido no próprio texto do
erro**.

> 🔴 **O gateway não vai espelhar a tabela inteira de limites da Meta.** Este teto de 3 itens entrou
> na entrada porque é uma constante **estrutural** do bloco que o próprio gateway monta (a lista de
> botões do JSON que sai daqui) — não porque "todo limite deve virar validação". Um espelho da tabela
> de limites da Meta envelheceria e passaria a mentir; a partir da T-141 você já recebe o campo e o
> número certos em `detalhe_meta` quando a Meta recusa, o que cobre a maioria dos casos sem exigir
> mais uma constante para manter em dia aqui.

### Template: header e botão

Um template pode ter três lugares onde entra dado seu, e o gateway monta os três:

| Campo do pedido | Vira, na Meta | Para quê |
|---|---|---|
| `variaveis` | `components[].type = "body"` | as variáveis posicionais do corpo (`{{1}}`, `{{2}}`…) |
| `cabecalho` | `components[].type = "header"` | o header do template: texto, imagem, vídeo ou documento |
| `botoes_template` | `components[].type = "button"`, `sub_type: "url"` ou `"quick_reply"` | o parâmetro dinâmico de um botão do template — sufixo de URL (token de rastreio) **ou** payload de resposta rápida (id de agendamento, etc.) |

> 🔴 **`botoes_url` FOI REMOVIDO em 2026-07-28.** Ele era o campo de botão de URL, sucedido
> por `botoes_template`, e um pedido que ainda o mande recebe **`400` com erro nomeado e a tradução
> pronta** — nunca é ignorado. Veja *Mudanças que quebram*, no fim deste documento.

Os três são opcionais e independentes. **Se você não manda nenhum, o pedido sai sem a chave
`components`** — e isso não é detalhe: a Meta recusa `components: []` em alguns templates, e a
diferença só aparece no envio real.

**Você NÃO manda `components` pronto.** Mandar o formato da Graph API é recusado com `400` e o erro
nomeado `components cru da Graph API nao e aceito`. O motivo é a razão de este gateway existir: o
esquema daqui torna *inexprimível* o que a Meta rejeita, e um repasse cru devolveria essa classe
inteira de erro para a sua produção — você montaria errado, a Meta recusaria, e o gateway não teria
protegido ninguém.

**`cabecalho`** — `tipo` é `"texto"`, `"imagem"`, `"video"` ou `"documento"` (o mesmo vocabulário de
`categoria`; `audio` e `sticker` não valem como header). Em `"texto"` mande `texto`; nos outros mande
`media_id`. `nome_arquivo` só existe em `"documento"`. Campo no tipo errado é `400` — ele sumiria em
silêncio, e você veria `200` com o header faltando no celular do cliente.

> ⚠️ **O header de mídia é por `media_id`, nunca por URL.** Não há campo de URL, e um `media_id` com
> forma de URL é recusado com `400`. URL crua faz a **Meta ir buscar** o arquivo, e quando ela não
> busca — host fora, TLS, `404` — **não há erro em lugar nenhum**: o template chega sem o documento.
> É exatamente a falha calada que fez `POST /v1/media` existir. Suba os bytes lá e use o id que ele
> devolve.

> ℹ️ **Nota de voz (PTT) não tem categoria própria: mande `categoria: "audio"`.**
> No canal **Cloud API**, nota de voz e áudio comum vão pelo **mesmo** caminho — `tipo: midia` com
> `categoria: audio`. Não existe categoria separada para PTT.
>
> **Quem vem de um sistema baseado em Baileys tende a procurar o equivalente do `sendWhatsAppAudio`,
> e o risco não é receber erro — é o oposto: o envio é aceito e não entrega.** Falha silenciosa, do
> tipo que só aparece quando alguém do outro lado diz "não recebi".
>
> *Origem, e ela importa porque muda o quanto isto vale:* a formulação acima é do consumidor
> um consumidor (2026-07-28), e o **"aceita e não entrega" é conhecimento HERDADO** do histórico dele
> com Evolution/Baileys — **não** é medição contra este gateway. O que foi medido aqui, em
> 2026-07-28, é o caminho que **funciona**: `voice` colapsado em `audio`, enviado pelo
> `POST /v1/messages`, entregue e ouvido no aparelho.
>
> **Em aberto, e escrito como pergunta de propósito:** o WhatsApp distingue PTT de áudio comum na
> **renderização** (bolinha de onda vs. arquivo). Ninguém conferiu como o aparelho renderizou o que
> chegou naquele teste — só que chegou e foi ouvido. Se você precisa da renderização de PTT
> especificamente, **isto não está respondido**, e responder por analogia aqui seria inventar.

**`botoes_template`** — uma **união discriminada por `tipo`**, no mesmo padrão de
`cabecalho`: cada item é `{"indice": …, "tipo": "url"|"resposta_rapida", …}`, e o campo que vem
depois de `tipo` muda com ele.

```jsonc
// o botão de URL — o mesmo componente que o campo removido `botoes_url` produzia
"botoes_template": [ {"indice": 0, "tipo": "url", "texto": "BR123456789BR"} ]

// e o que só existe aqui: o botão de resposta rápida, com payload seu
"botoes_template": [ {"indice": 1, "tipo": "resposta_rapida", "payload": "confirma:41"} ]
```

> ⚠️ **A Meta chama isto de `button`, e o pedido original desta tarefa pedia o nome `botoes` — mas o
> campo de verdade se chama `botoes_template`.** `Pedido.Buttons`
> (json `"botoes"`) já existe desde antes, para `"tipo": "botoes"` (mensagem interativa comum,
> `{"id","titulo"}`) — e duas structs Go com a MESMA tag JSON no mesmo nível são **ignoradas em
> silêncio** tanto por `Marshal` quanto por `Unmarshal` (confirmado por experimento antes de
> escrever este campo: nenhum erro, os dois campos ficam vazios). Reusar `"botoes"` aqui apagaria
> as duas funcionalidades ao mesmo tempo, sem sinal nenhum — a própria armadilha-mãe deste projeto
> (*"a regra vale aqui, não vale ali"*), desta vez dentro do `encoding/json`. Por isso: **use
> `botoes_template`** nos seus pedidos novos.

- `"tipo": "url"` — leva `texto`; produz **exatamente** o mesmo componente que o campo removido
  `botoes_url` produzia (mesma forma de bloco, mesmo `sub_type`, congelado em teste byte a byte).
  Migrar um botão de URL não muda nada que a Meta veja.
- `"tipo": "resposta_rapida"` — leva `payload` em vez de `texto`; vira `sub_type: "quick_reply"`
  com o parâmetro `{"type": "payload", "payload": …}`. **É o caminho que faltava**: sem ele, um
  clique no botão de um template volta como o **texto** do botão ("Sim"), não um id que o seu
  sistema reconheça — e um fluxo que espera `confirma:41` não acha nada para casar.

> ⚠️ **`"resposta_rapida"` ainda não passou por tráfego real da Meta** (situação em 2026-07-28,
> revista na remoção de `botoes_url`). Está provado por suíte e por mutação — o teste distingue
> `type:"payload"` de `type:"text"`, não só a presença do parâmetro. Mas *sucesso de API ≠ sucesso
> de efeito* é armadilha registrada deste projeto, e nenhum template com botão de resposta rápida
> **e payload dinâmico** foi enviado por este caminho até agora: o consumidor que chegou mais perto
> tem quick reply **estático** (sem parâmetro, logo sem componente emitido).
>
> **A prova é sua, e ela cabe em cinco minutos:** mande um template com `resposta_rapida` para um
> número seu, **clique no botão no aparelho** e olhe o evento de entrada. Se `botao_payload` vier com
> o seu payload, está provado para o seu caso; se vier o **texto** do botão, o caminho não faz o que
> você precisa e você descobriu isso antes de um cliente descobrir. Faça esse teste **antes** de ligar
> qualquer fluxo que case por payload — este documento não pode confirmar por você o que ninguém
> observou ainda.
>
> **A metade `"url"`, essa sim, tem prova de aparelho:** em 2026-07-28, ao confirmar que podia
> largar `botoes_url`, um consumidor enviou um template pelo `botoes_template` e **clicou no botão
> no celular**, com o portal abrindo. Ele mediu junto que a janela de 24 h estava **fechada** (57 h
> desde o último inbound) — sem esse controle, a mensagem poderia ter saído como texto livre e o
> teste provaria outra coisa.

> **Uma lista só, com `tipo` por item, mesmo que o seu catálogo de hoje não precise disso.** Um
> consumidor levantou o catálogo real dele (2026-07-26, direto da Meta): **90 templates aprovados,
> 38 com botão, 51 botões `QUICK_REPLY` e 17 `URL` — e nenhum template misturando os dois tipos.**
> Ou seja: se você "simplificar" isto um dia separando por campo, o catálogo atual **não vai te
> contradizer**. Ele também não te autoriza: isso é observação sobre os templates que existem hoje,
> **não é garantia da Meta** — nada impede que o próximo template aprovado seja misto. A união
> discriminada cobre esse dia sem ninguém se lembrar de nada; dois campos com checagem cruzada
> dependem de alguém lembrar. Foi por isso que a lista é uma só.

`indice` é a posição do botão **no template**, começando em `0`, na ordem em que os botões foram
declarados na Meta — não a posição na lista que você manda. Índice negativo é `400`. **Dois
parâmetros para o mesmo índice também é `400`**: é o mesmo botão declarado duas vezes, a Meta
descartaria um dos dois parâmetros em silêncio, e não seríamos nós que escolheríamos qual.

> ℹ️ **Até 2026-07-28 essa checagem tinha de atravessar dois campos** (`botoes_template` e o antigo
> `botoes_url`), porque os dois escreviam no mesmo espaço de índices. Com `botoes_url` removido, é
> **uma lista, um espaço de índices** — não há mais como distribuir botões do mesmo template entre
> dois lugares, nem por engano.

`payload` é **opaco** para o gateway — pode carregar um id interno seu (ex.: `"confirma:41"`). Ele
**nunca aparece em log**: a mensagem de erro nomeia o campo, nunca o valor.

> ⚠️ **Índice errado não dá erro nenhum.** O token vai para o botão errado, o cliente clica e cai no
> lugar errado. Confira a ordem dos botões no catálogo (`GET /v1/templates`) antes de fixar o número.

> ⚠️ **A numeração do botão é dele, não do corpo.** Num template cuja URL cadastrada é
> `https://cliente.exemplo.com.br/{{1}}` **e** cujo corpo também usa `{{1}}`, os dois `{{1}}` são
> coisas diferentes. O `texto` do botão preenche o `{{1}}` **da URL**; `variaveis[0]` preenche o
> `{{1}}` **do corpo**. Confundir os dois manda o nome do cliente para dentro do link.

**`botoes_url` — REMOVIDO em 2026-07-28.** Ele nasceu como o campo de botão de URL, foi sucedido por
`botoes_template` e conviveu com o sucessor por dois dias. **`botoes_url` era o nome do CAMPO, não da
funcionalidade:** o botão de URL não foi a lugar nenhum — ele é `{"tipo": "url"}` dentro de
`botoes_template`, e produz o mesmo componente byte a byte. Um pedido que ainda mande `botoes_url`
recebe `400`. Veja *Mudanças que quebram*.

Os exemplos abaixo têm a **forma** de templates aprovados de verdade: a contagem de variáveis de cada
um foi conferida contra `GET /v1/templates` de uma conta real em 2026-07-26, então eles não são forma
inventada. **Os nomes de template são de exemplo** — os seus são os do seu catálogo, e é o
`GET /v1/templates` da sua instância que diz quantas variáveis e quantos botões cada um tem.

Exemplo — **header-documento** (`venda_confirmada`: `HEADER DOCUMENT`, três variáveis de corpo,
nenhum botão):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "template",
  "template": "venda_confirmada",
  "idioma": "pt_BR",
  "cabecalho": {
    "tipo": "documento",
    "media_id": "1234567890123456",
    "nome_arquivo": "comprovante-4210.pdf"
  },
  "variaveis": ["Maria", "4210", "R$ 349,90"]
}
```

Exemplo — **botão de URL** (`equipamento_enviado`: quatro variáveis de corpo e **um** botão URL, no
índice `0`):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "template",
  "template": "equipamento_enviado",
  "idioma": "pt_BR",
  "variaveis": ["Maria", "OS-1187", "Correios", "BR123456789BR"],
  "botoes_template": [
    {"indice": 0, "tipo": "url", "texto": "BR123456789BR"}
  ]
}
```

Exemplo — **botão de resposta rápida** (um template de confirmação de agendamento com um botão de
`quick_reply` no índice `0`, cujo clique volta com o `payload` — não o texto "Sim" — no evento de
entrada):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "template",
  "template": "confirma_agendamento",
  "idioma": "pt_BR",
  "variaveis": ["Maria", "terça, 14h"],
  "botoes_template": [
    {"indice": 0, "tipo": "resposta_rapida", "payload": "confirma:41"}
  ]
}
```

Exemplo — **corpo + botão** (`orcamento_disponivel`: uma variável de corpo e o botão do portal no
índice `0`):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "template",
  "template": "orcamento_disponivel",
  "idioma": "pt_BR",
  "variaveis": ["Maria"],
  "botoes_template": [
    {"indice": 0, "tipo": "url", "texto": "tok-portal-9f2"}
  ]
}
```

> **O gateway aceita os três blocos juntos** — a ordem `header → body → button` está implementada e
> testada. Mas se você está escrevendo `botoes_template` com **dois itens**, confira o catálogo
> antes: template com mais de um botão é raro, e dois itens contra um botão só cairiam no mesmo
> lugar.

Um template só com `variaveis` continua funcionando exatamente como antes: o corpo que vai para a
Meta é byte a byte o mesmo, e o hash de idempotência dele também (os dois estão presos por teste de
não-regressão). Você não precisa mudar nada no que já manda.

### `contatos` — e o campo que decide se o cartão SERVE (2026-08-20)

Um cartão de contato tem **um** campo obrigatório, `name.formatted_name`. Mas há um campo opcional que
muda o que o cliente pode fazer com o cartão, e **a documentação da Meta não diz isso**: ela lista
`wa_id` como mais um campo entre outros.

| o que você manda | o que aparece no aparelho |
|---|---|
| `phones: [{"phone": "+55 (32) 99999-0000", "type": "WORK"}]` | botão **"Convidar para o WhatsApp"**, foto genérica |
| o mesmo, mais `"wa_id": "5532999990000"` | botão **"Conversar"**, e a **foto de perfil** do contato |

*Medido em aparelho em 2026-08-20, com os dois cartões idênticos exceto por esse campo.*

🔴 **Sem `wa_id`, o cartão não serve para o caso de uso principal.** Passar o contato de um vendedor
para o cliente falar com ele vira um convite para instalar o WhatsApp — o cliente **já está** no
WhatsApp, lendo a sua mensagem. Quem só preenche `phone` acha que mandou um contato e mandou um
convite.

> ℹ️ **O `wa_id` é o número no formato canônico da Meta, sem `+`, sem espaço e sem pontuação** —
> `5532999990000`. Ele é o mesmo valor que chega para você no `de` de uma mensagem recebida. O `phone`
> ao lado pode ser escrito como você quiser (é o que o cliente vê no cartão); o `wa_id` é o que a
> Meta usa para achar a conta.

### Reação e localização

O vocabulário destes dois campos é **o mesmo do lado de entrada** (`reacao {emoji, alvo}`,
`localizacao {latitude, longitude, nome, endereco}` — ver *O que você recebe*, acima): enviar e
receber com nomes diferentes para a mesma coisa seria o começo de dois vocabulários.

**`tipo: "reacao"`** — aplica um emoji a uma mensagem que você recebeu antes.

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "reacao",
  "reacao": { "alvo": "wamid…", "emoji": "👍" } }
```

`alvo` é o `wamid` da mensagem reagida (o `wa_message_id` que veio no evento). `alvo` é sempre
**obrigatório**. `emoji` também é obrigatório **como chave** — mas o **valor** pode ser vazio, e os
dois casos significam coisas diferentes:

- **`emoji` com um valor** — adiciona (ou substitui) a reação.
- **`emoji: ""`** (string vazia, chave presente) — **remove** a reação.
- **`emoji` ausente** (chave não mandada) — `400`, erro nomeado de campo obrigatório. **Não** é
  tratado como remoção.

```jsonc
// remove a reação
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "reacao",
  "reacao": { "alvo": "wamid…", "emoji": "" } }
```

> ⚠️ **Por que `emoji` vazio remove mas `emoji` ausente não — a assimetria com o RECEBIMENTO é
> deliberada, não um bug.** No RECEBIMENTO (ver *`reacao`, `voz`, ...* acima) é a própria Meta que
> monta o evento, e ela *omite* a chave `emoji` para dizer "o usuário removeu". No ENVIO quem monta
> o pedido é um **programa seu**, e "esqueci de mandar o campo" é o erro de programação mais comum
> que existe — chave ausente é indistinguível de descuido. String vazia é diferente: é uma escolha
> que alguém digitou de propósito. Por isso só a string vazia remove; a chave ausente continua
> `400`.
>
> **A fonte da remoção não é a documentação da Meta — é um experimento com aparelho.** A doc oficial
> (developers.facebook.com/docs/whatsapp/cloud-api/messages/reaction-messages, lida em 2026-07-26)
> lista `<EMOJI>` como *"Required"* e não descreve nenhum formato de remoção. Um consumidor fez o
> experimento em 2026-07-26 (10:15 -03), com alguém olhando o aparelho: dois envios pela Graph API
> direta, mesmo corpo, só o `emoji` mudando — `"👍"` fez a reação **aparecer**; `""` fez a reação
> **sumir**. Nos dois casos a Meta respondeu `200` com um `wa_message_id` **novo** — se a reação não
> tivesse sumido no segundo envio, a resposta teria sido idêntica. O `200` prova que a Meta aceitou o
> pedido, nunca que o efeito aconteceu; a única testemunha possível era o aparelho. Se você tem uma
> forma melhor de confirmar remoção que não seja olhar o aparelho, ela não existe hoje neste
> gateway nem na doc da Meta.

**`tipo: "localizacao"`** — compartilha um ponto com o destinatário.

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "localizacao",
  "localizacao": { "latitude": 37.44, "longitude": -122.16, "nome": "…", "endereco": "…" } }
```

`latitude` e `longitude` são **obrigatórios**; `nome` e `endereco` são opcionais. Os dois nomes de
campo na Graph API (`name`, `address`) são traduzidos por este gateway — você não os usa.

> ⚠️ **`0` é coordenada válida** (o cruzamento do meridiano de Greenwich com o equador), e ela SAI no
> corpo — nunca é omitida por parecer "vazia". O que é recusado com `400` é a *ausência* de
> `latitude`/`longitude`, não o valor `0`.

**`tipo: "pedir_localizacao"`** — mostra um texto com um botão que abre a tela de compartilhar
localização do WhatsApp (`location_request_message` da Cloud API).

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "pedir_localizacao",
  "texto": "Pode compartilhar sua localizacao para a entrega?" }
```

A forma inteira é só isto — `texto` é o único campo, e vira `body.text`. **Este tipo não tem
cabeçalho nem rodapé**: `cabecalho_texto` e `rodape` são `400` aqui, mesma regra de campo proibido
dos outros tipos que não os usam — a Cloud API não documenta nenhum dos dois para este objeto.
Quando o cliente responde tocando no botão, a localização compartilhada chega para você pela
**mesma rota de sempre**: o evento `localizacao` no webhook (ver *O que você recebe*, acima) — é o
que fecha o ciclo e o que faz valer a pena pedir por este tipo em vez de por texto livre.

**`tipo: "flow"`** — abre um WhatsApp Flow (um formulário nativo dentro do WhatsApp) para o
destinatário preencher (T-154).

🟢 **FORMA CONFIRMADA CONTRA O PARSER DA META EM 2026-08-20 (T-156)** — mas leia com cuidado o que
isso cobre, porque este projeto já confundiu as duas coisas três vezes só naquele dia. Enviamos um
`tipo:"flow"` com `flow_id` **deliberadamente inventado**, nos dois ramos de `acao` (`navigate` com
`tela`, e `data_exchange` sem). Nos dois, a Meta respondeu:

```
400  codigo_meta 131009
detalhe_meta: Parameter "flow_id" is invalid. Please check if the flow associated to
              this id belongs to your WhatsApp Business Account, and it's in a valid state.
```

Ou seja: ela **parseou o payload inteiro** e só parou no único campo que era falso de propósito.
`flow_message_version`, `flow_token`, `flow_cta`, `flow_action` e `flow_action_payload` (com
`screen` e `data`) **atravessaram o parser dela** — se algum estivesse errado ou com nome trocado,
ela teria reclamado dele antes de chegar no `flow_id`.

🔴 **O que isto NÃO prova: a RENDERIZAÇÃO.** A página oficial de Flows
(`developers.facebook.com/docs/whatsapp/flows/...`) monta o conteúdo **no cliente**, via
JavaScript, e nunca existiu um Flow publicado neste WABA — então nenhuma tela chegou a abrir do
lado do destinatário. "A Meta aceitou o payload" e "o Flow renderizou no cliente" são provas
**diferentes**, e só a primeira aconteceu. Não leia esta seção como "provado" sem essa distinção.

E os **parâmetros** continuam vindo de outro lugar: a estrutura dos campos abaixo (nomes,
obrigatoriedade, XOR de `id`/`nome`) veio de **documentação de BSP (360dialog) e de SDK de
terceiro (whatsapp-api-js)**, lidos em 2026-08-20 — a Meta não confirmou que essa fonte é oficial,
só que a **combinação** que montamos a partir dela passa no parser dela. São duas coisas que este
contrato não pode fundir: proveniência dos **parâmetros** (terceira mão, como sempre foi) e
confirmação da **forma** (agora real).

```jsonc
{ "instancia": "lojinha", "para": "5511999990000", "tipo": "flow",
  "texto": "Preencha seus dados para agendar",
  "botao_titulo": "Agendar",           // flow_cta — reusado de cta_url/lista, máximo 20 (teto de TERCEIRA MÃO, ver acima)
  "fluxo": {
    "id": "123456789",                 // OU "nome" — nunca os dois, e um dos dois é obrigatório
    "token": "agendamento-4471",       // obrigatório: identifica a resposta do Flow para você
    "acao": "navigate",                // "navigate" | "data_exchange" — opcional, default "navigate"
    "tela": "TELA_INICIAL",            // obrigatória quando acao é "navigate"
    "dados": {"nome_cliente": "Maria"} // opcional
  } }
```

`fluxo.id` e `fluxo.nome` são **mutuamente exclusivos** — mande um ou o outro, nunca os dois, e ao
menos um é obrigatório; `400` nomeia o campo tanto quando os dois vêm juntos quanto quando os dois
faltam. `fluxo.token` é **obrigatório**: é o valor que você gera e que casa a resposta do Flow (ver
abaixo) com o que você estava fazendo quando o abriu — sem ele a resposta volta e não há como saber
de quem é. `fluxo.tela` é obrigatória **só** quando `fluxo.acao` é `"navigate"` (o default); em
`"data_exchange"` ela é opcional.

O teto de 20 caracteres de `botao_titulo` (`flow_cta`) aqui é uma **constante própria**, não
compartilhada com a de `cta_url` (`display_text`) nem com a de `lista` (`action.button`) — são
**três campos diferentes** da Cloud API que hoje coincidem em valor por acaso, e a Meta pode mudar
qualquer um dos três sem mudar os outros (regra fixada pela T-149, estendida aqui pela T-154).
**Este número continua de terceira mão** — a chamada da T-156 confirmou que o *campo* `flow_cta`
atravessa o parser da Meta, não que o *limite* de 20 caracteres está certo (o valor enviado no
teste nunca chegou perto da fronteira). Só uma medição por bissecção, como a de `cta_url` acima,
corrige este número se ele estiver errado.

> ⚠️ **Este tipo NÃO aceita `cabecalho_texto` nem `rodape`.** A recusa é por **FALTA DE
> CONFIRMAÇÃO**, e não porque a Meta proíba — a diferença importa para quem for reabrir o assunto
> depois: as fontes de terceiro divergem sobre o Flow suportar header/footer, e nenhuma delas foi
> confirmada nesta leitura. Recusar agora é **aditivo depois**, se algum dia alguém confirmar que a
> Meta aceita; aceitar agora e estar errado seria **quebra de contrato** depois.

> ℹ️ **A resposta do Flow chega pelo webhook como `interactive` do subtipo `nfm_reply`, e este
> gateway AINDA NÃO a enriquece.** Ela chega com `sub_tipo` cru (`"interactive"`), sem
> `botao_payload`/`botao_texto` preenchidos — o mesmo tratamento que qualquer subtipo de
> `interactive` que o parser ainda não modela recebe hoje (ver *O que você recebe*, acima). Não há
> enriquecimento nenhum: se você precisa do conteúdo da resposta antes de o gateway passar a
> modelá-la, leia o corpo cru do webhook.

**Exemplos executados** (desserializados com `json.Unmarshal` num `Pedido`, validados com
`Validar()` e reserializados; há teste travando por
teste que o corpo montado tem exatamente esta forma):

Reação:

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "reacao",
  "reacao": {
    "alvo": "wamid.TESTE001",
    "emoji": "👍"
  }
}
```

vira, no corpo para a Graph API:

```json
{
  "messaging_product": "whatsapp",
  "reaction": {
    "emoji": "👍",
    "message_id": "wamid.TESTE001"
  },
  "recipient_type": "individual",
  "to": "5511999990000",
  "type": "reaction"
}
```

Reação **removida** (mesmo `alvo`, `emoji` vazio):

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "reacao",
  "reacao": {
    "alvo": "wamid.TESTE001",
    "emoji": ""
  }
}
```

vira, no corpo para a Graph API — note que a chave `"emoji"` **sai vazia, nunca omitida**:

```json
{
  "messaging_product": "whatsapp",
  "reaction": {
    "emoji": "",
    "message_id": "wamid.TESTE001"
  },
  "recipient_type": "individual",
  "to": "5511999990000",
  "type": "reaction"
}
```

Localização:

```json
{
  "instancia": "lojinha",
  "para": "5511999990000",
  "tipo": "localizacao",
  "localizacao": {
    "latitude": 37.44221496582,
    "longitude": -122.16165924072,
    "nome": "Cafe de Teste",
    "endereco": "Rua de Teste, 101"
  }
}
```

vira, no corpo para a Graph API:

```json
{
  "location": {
    "address": "Rua de Teste, 101",
    "latitude": 37.44221496582,
    "longitude": -122.16165924072,
    "name": "Cafe de Teste"
  },
  "messaging_product": "whatsapp",
  "recipient_type": "individual",
  "to": "5511999990000",
  "type": "location"
}
```

### Três coisas que o contrato recusa de propósito

As três guardas abaixo nascem de incidentes reais desta rede — o custo já foi pago noutro sistema —,
não de uma leitura da documentação da Meta. Elas não foram conferidas na fonte da Meta; o rótulo em
cada uma diz isso de propósito, para não virarem "documentado pela Meta" sem terem sido.

- **`responder_a` em `template`** → `400`. *Observado em produção nesta rede; não conferido na doc da
  Meta:* a Meta aceita e responde `200`, e a bolha de citação nunca renderiza. Não há erro em lugar
  nenhum; a única evidência seria o seu cliente vendo uma resposta solta.
- **Botão de resposta junto com botão de link** → `400`. *Observado em produção nesta rede; não
  conferido na doc da Meta:* na Cloud API os dois não convivem no mesmo interativo; sem esta guarda
  seria um `400` da Meta descoberto em produção.
- **Conteúdo em base64** → `400`, com o caminho no erro. *Observado em produção nesta rede; não
  conferido na doc da Meta:* a Cloud API não aceita; um sistema desta rede descobriu isso com um envio
  que **falhava calado**.

### 🔴 O que é ESTÁVEL neste corpo de erro, e o que não é (2026-08-20, ampliado com T-153)

Um corpo de erro tem sete campos, e eles **não têm o mesmo compromisso**. Casar no campo errado é o
tipo de acoplamento que não falha no dia em que é escrito — falha meses depois, num deploy nosso que
não quebrou nada para mais ninguém.

| campo | compromisso |
|---|---|
| `classe` | **ESTÁVEL.** É o vocabulário fechado (`retentavel`, `permanente`, `config`, `desconhecido`), e é ele que decide se você retenta |
| `codigo_meta` | **ESTÁVEL** enquanto a Meta o mantiver — é dela, e repassamos cru |
| `mensagem` | ❌ **NÃO É ESTÁVEL.** Texto para humano. A redação muda sem aviso e **sem bump de MAJOR** |
| `detalhe_meta` | ❌ **NÃO É ESTÁVEL, em dobro** — é texto de terceiro, cru da Meta, e pode nem vir |
| `subcodigo_meta` | **ESTÁVEL** enquanto a Meta o mantiver — é `error.error_subcode` dela, cru, mesmo compromisso de `codigo_meta` |
| `explicacao_meta` | ❌ **NÃO É ESTÁVEL.** É `error.error_user_msg` (com `error.error_user_title` como prefixo, quando vem) — texto humano da Meta, pode mudar de redação ou parar de vir sem aviso |
| `rastro_meta` | **OPACO e ESTÁVEL enquanto durar a chamada** — é o `fbtrace_id`, a ÚNICA coisa deste corpo que o suporte da Meta aceita para abrir chamado sobre uma chamada específica, e **ele NÃO VOLTA depois desta resposta**. Não decida nada por ele; **guarde-o quando um erro te importar** |

**Decida por `classe`; quando precisar de granularidade, por `codigo_meta` ou `subcodigo_meta`. Nunca
por frase — nem `mensagem`, nem `explicacao_meta`.**

🔑 **Campo vazio aqui é DADO, não buraco — e a distinção só existe desde a T-153.** Medido pelo
`consumer-b` em 2026-08-20: um `503 codigo_meta 2` na criação de template veio com
`subcodigo_meta` e `explicacao_meta` **vazios**, e o `rastro_meta` presente. Isso não é o gateway
perdendo campo: é a Meta não ter mandado nem `error_subcode` nem `error_user_msg`.

*Por que vale registrar:* antes da T-153 o gateway descartava esses campos, então "a Meta calou" e
"nós comemos o campo" eram **indistinguíveis** do lado de fora. Agora um `2` genérico sem subcódigo
significa exatamente *a Meta errou por dentro e não sabe dizer o quê* — e aí o único caminho é o
`rastro_meta`, que é o que o suporte dela aceita.

*Isto não é hipotético: em 2026-08-20 a mensagem de um erro de limite mudou de `"campo acima do limite
de caracteres"` para `"campo acima do limite"`, porque a antiga dizia "caracteres" enquanto contava
itens de lista — ela mandava consertar a coisa errada. Foi uma correção, saiu numa MINOR, e vai
acontecer de novo sempre que uma mensagem estiver errada. **Mensagem de erro é para ser consertada;
prendê-la num contrato é escolher manter o texto ruim.***

> ℹ️ **Se você precisa reagir a um caso específico, peça um CÓDIGO ou um CAMPO — não uma frase.**
> A formulação é do consumidor `consumer-b` (2026-08-20), e ela está certa: uma frase que virou
> interface é uma frase que ninguém pode mais melhorar. Pedido desse tipo é bem-vindo e tem resposta
> rápida.

### O que cada erro quer dizer

```jsonc
{ "erro": { "classe": "retentavel" | "permanente" | "config" | "desconhecido",
            "codigo_meta": 131047, "mensagem": "…", "detalhe_meta": "…" } }
```

**Decida pela `classe`, nunca pelo status HTTP.** O status é dica de transporte, não o contrato: a
mesma classe sai em mais de um status, porque cada guarda da cadeia de envio (autenticação, vínculo
com a instância, corpo do pedido, idempotência, chamada à Meta) devolve o status que faz sentido
*naquele ponto* — não existe um status fixo por classe.

🔴 **`detalhe_meta` (T-141, `POST /v1/messages`) é passthrough CRU do que a Meta manda em
`error.error_data.details` — leia antes de usar.** Ele só aparece (campo ausente do JSON quando não
há detalhe, nunca `""`) quando o erro veio de uma resposta direta da Meta a este envio, e:

- **é texto de terceiro, não nosso** — pode ecoar pedaço do seu próprio payload (telefone, texto da
  mensagem) da mesma forma que `error_data` da Meta pode fazer; por isso **não deve ser logado nem
  exibido a terceiro sem cuidado** do seu lado, pela mesma razão que este gateway não guarda esse
  campo no próprio log de trânsito;
- **truncado em 500 runas**, com o sufixo ` …[truncado]` quando isso acontece;
- **não é garantido em toda recusa da Meta.** Se ela ainda manda `error_data.details` pelo caminho de
  hoje é questão em aberto até medição contra produção — trate a ausência do campo como "não veio
  desta vez", nunca como "a Meta não teria dito".

| Status | Classe | Quando acontece |
|---|---|---|
| `400` | `permanente` | falta o corpo, o corpo não é JSON, ou falha a validação do esquema; **ou** a Meta recusou o pedido com um erro que retry não resolveria |
| `400` | `retentavel` | o corpo não chegou inteiro (sua conexão caiu no meio do upload). Mesmo status, classe diferente — é exatamente por isso que se decide pela `classe` |
| `401` | `config` | seu `Authorization` está ausente ou é inválido |
| `403` | `config` | a instância pedida no corpo não é sua |
| `404` | `config` | a instância pedida não existe |
| `409` | `retentavel` | outro envio seu com a **mesma** `Idempotency-Key` está em andamento — ou uma tentativa anterior com ela terminou em `desconhecido` e a chave ficou retida (veja abaixo) |
| `413` | `permanente` | o corpo passou do limite de tamanho aceito (**1 MiB** por default — ver *Limites conhecidos*) |
| `422` | `permanente` | esta `Idempotency-Key` já foi usada para um pedido **diferente**. Trocar a chave é o conserto; repetir nunca vai funcionar |
| `502` | `config` | a configuração gravada para essa instância está inválida — a Meta recusou a credencial (token/permissão), **ou** o `phone_number_id` cadastrado tem forma inválida e o pedido nem chegou a sair. Não é o `Authorization` que você mandou, e reenviar não resolve. **Quem conserta é você**, veja abaixo |
| `502` | `desconhecido` | o gateway não obteve uma resposta utilizável da Meta (transporte, prazo da instância estourado, ou `2xx` sem id): **não se sabe** se a mensagem saiu. Veja a seção abaixo antes de reenviar |
| `503` | `retentavel` | o gateway não conseguiu falar com o próprio armazenamento, a instância está pausada, **esta instância do gateway não detém a liderança do par** (v0.47.0 — ver abaixo), **ou** a Meta devolveu um erro classificado como retentável (5xx, timeout, ou throttling — ver nota abaixo) |

`retentavel`: reenfileire e tente de novo mais tarde. `permanente`: **não tente de novo** — conserte
o pedido. `desconhecido`: **não reenvie no automático** — a mesma chave vai dar `409`, e trocá-la pode
duplicar; leia a próxima seção.

> **Como o throttling da Meta vira `retentavel`, e o limite disso (T-142):** a Meta **não documenta**
> com que status HTTP o erro de limite de taxa chega — a página oficial de códigos de erro lista a
> família de throttling sem coluna de status, e a API de Marketing Messages mostra um erro do mesmo
> feitio como `400`. Por isso o gateway não confia no status para reconhecer throttling: ele reconhece
> pelo **código de erro da Meta**, contra uma lista **nossa e conservadora** de códigos verificados na
> documentação oficial. Um código de throttling que a Meta inventar amanhã cai em `permanente` até
> alguém acrescentar essa lista — não é garantia de cobertura total, é o que está verificado hoje.


🔎 **E o `GET /v1/estado` publica o bloco `lideranca` (v0.49.0)**, com quatro campos: `armada` (há
guarda configurada), `estado` (`observado` quando armada, `nao_se_aplica` quando não), `titular`
(`true`/`false`, e **`null` quando desarmada** — um nó único não venceu eleição nenhuma) e `motivo`
(só quando `titular: false`). ⚠️ **Este bloco vale menos para você do que
a primeira versão desta seção afirmava, e a correção é nossa:** o tráfego chega por um VIP, e quem
atende pelo VIP é **por construção** o nó que detém a liderança. Então, na prática, você quase sempre
verá `titular: true` — o nó que **não** é titular não detém o VIP e por isso nem recebe as suas
requisições. *A leitura útil para você é a janela curta entre o VIP migrar e a concessão ser
adquirida: ali você pode receber `503 retentavel` e este bloco explica por quê.* **Conferir se o par
inteiro está protegido é obrigação de quem opera o gateway, não sua** — exige falar com cada nó pelo
endereço próprio, o que o VIP não permite. Como todo campo desta rota, ele é **aditivo**:
o formato só cresce.

> 🔴 **Uma leitura errada anula o bloco inteiro, então ela fica escrita aqui:** perguntar
> *"o gateway está armado?"* só funciona **com o gateway de pé**. Se a rota não responder — timeout,
> conexão recusada, `5xx` —, isso é **"não consegui verificar"**, e nunca `armada: false` nem
> "está tudo bem". Os dois desfechos produzem a mesma ausência de resposta, e tratá-los igual
> transforma um indicador de segurança em silêncio. *Regra trazida pelo time do `consumer-b` em
> 2026-08-19, ao desenhar o alarme deles em cima deste campo.*

🔵 **A `503` da liderança (v0.47.0) — o que ela é, e por que você não precisa fazer nada diferente.**
O gateway está sendo preparado para rodar num **par ativo-passivo**. Nesse arranjo, apenas a instância
que detém a liderança pode **enviar**; se a que te atendeu não for a titular, ela responde `503
retentavel` com uma mensagem dizendo isso, **em vez de mandar a mensagem**.

**Por que isso te protege, e não te atrapalha:** o risco do par não é uma mensagem a menos — é a
**mesma mensagem duas vezes** no celular da sua cliente, com a cota de 250/dia queimando em dobro,
caso as duas instâncias enviem. A recusa é o lado seguro.

**O que você faz:** exatamente o que já faz com qualquer `retentavel` — repita. Quando repetir, o
endereço já terá migrado para quem detém a liderança, e o envio sai normalmente. **Não troque a
`Idempotency-Key`**: a mensagem não foi enviada, então repetir com a MESMA chave é o comportamento
correto e não duplica.

*Hoje esta guarda está **desarmada** (o gateway roda em nó único), então esta resposta **não acontece**
— ela está documentada agora para que, no dia em que o par existir, ela não chegue como surpresa. Se
você quiser tratá-la de forma distinta das outras `503`, a mensagem do erro é reconhecível: ela diz
"não detém a liderança do par". Mas decidir pela `classe`, como sempre, continua sendo suficiente.*

🔴 **`config` não quer dizer "espere alguém consertar", e essa é a correção mais importante desta
tabela.** A palavra é herança de quando as credenciais eram cadastradas por quem opera o gateway.
**Hoje quem cadastra é você**, então `config` se divide em dois, com donos diferentes:

| O que veio | De quem é o conserto | O que fazer |
|---|---|---|
| `502 config` — a Meta recusou o `token_envio`, ou o `phone_number_id` cadastrado tem forma inválida | **seu**: são valores que você mandou no `POST /v1/cadastro` | corrija na sua conta Meta e **recadastre** o conjunto completo. Se a janela de 24 h já fechou, o `POST` responde `409` e aí sim é preciso pedir reabertura a quem te entregou o slug |
| `401 config` — `Authorization` ausente ou desconhecido | **seu**: é o token de consumidor do item 5 do pacote | confira o header. Se o token foi rotacionado, use o novo |
| `403 config` — a instância não é sua · `404 config` — a instância não existe | **de quem opera o gateway**: é o vínculo consumidor↔instância | confira primeiro se você não escreveu o **seu** nome em `instancia` em vez do slug — é o erro mais comum. Se o slug estiver certo, é o único caso desta tabela que você não resolve sozinho |

*Ou seja: das quatro respostas `config` da tabela acima, **duas são inteiramente suas** (`401` e
`502`) e as outras duas ainda pedem uma conferência sua antes de você concluir que o problema é
nosso. Parar e esperar sem olhar a própria configuração é a reação errada na maioria dos casos.*

Para os erros que nascem **antes** de falar com a Meta (autenticação, vínculo com a instância, corpo
do pedido, idempotência), a classificação é decidida por este gateway, não pela Meta, e o
`codigo_meta` vem `0`. O `desconhecido` também nasce deste lado (`codigo_meta` `0`): por definição, a
Meta não respondeu nada classificável. Só quando o erro **vem** da Meta (a linha `502 config`, a parte
da `503` que é dela, e a parte da `400 permanente`) é que a classe é derivada do status HTTP que a
Meta devolveu — e o `codigo_meta` viaja junto
para quem tiver regra própria; nós não decidimos nada por ele, porque inventar significado para código
não verificado seria pior que não mandá-lo.

### Falhou: a sua chave volta a valer, ou não?

Depende de o gateway **saber** se a mensagem foi criada do lado da Meta, e a `classe` do erro te diz
qual dos dois mundos você está. Não é a mesma pergunta que "deu erro?".

- **A Meta respondeu** que não deu certo — `classe` `permanente`, `config` ou `retentavel` vinda dela
  (`400`, `502 config`, a parte da `503` que é dela): a mensagem **não** foi criada. A chave **volta a
  valer** na hora, e um retry com ela envia de verdade. Isso vale inclusive para o `5xx` da Meta: ela
  respondeu, mesmo que com erro.
- **A Meta não respondeu nada utilizável** — `classe` `desconhecido` (`502`): transporte caiu, o prazo
  **da instância** estourou, ou veio `2xx` **sem id**. A mensagem **pode ter saído**. A chave fica
  **retida**, e um novo envio com ela recebe `409` até o TTL expirar.

A retenção é deliberada e você precisa saber conviver com ela: liberar a chave nesse caso faria o seu
retry legítimo criar uma **segunda** mensagem no celular de um cliente real — o dano que a
idempotência inteira existe para impedir. Um `409` que exige alguém olhando custa menos que uma
duplicata que ninguém vê.

#### 🔴 O que fazer com `desconhecido` (ou com um `409` que não passa) — o procedimento inteiro

**Não troque a chave por uma nova só para "destravar": isso reenvia às cegas**, e é justamente o caso
em que a mensagem pode já estar no celular de alguém. A chave fica retida até o TTL da idempotência
expirar (**default 72 h**), e todo envio com ela recebe `409` nesse período. Esse é o pior beco deste
contrato, então aqui está a saída, passo a passo, **sem depender de ninguém**:

**Passo 1 — olhe o seu próprio webhook, que é onde a resposta está.** Se a Meta criou a mensagem, ela
manda um evento `status` com `status: "sent"` para a sua `callback_url`, tipicamente em segundos.
Procure, na sua base de eventos recebidos, um `status` com:

- `para_canonico` igual ao destino daquele envio, **e**
- `timestamp` (ou o seu instante de recebimento) dentro da janela do envio que ficou `desconhecido`.

**Achou → a mensagem SAIU.** Grave o `wa_message_id` daquele evento como o id do seu envio (é o mesmo
que o `200` teria devolvido) e **não repita**. O `409` para de incomodar quando o registro expirar; até
lá, ele está certo.

**Passo 2 — não achou depois de alguns minutos?** Então a mensagem provavelmente não foi criada — mas
"provavelmente" é o melhor que existe aqui, e o gateway não vai fingir o contrário. A decisão é sua e
depende do que custa mais no seu negócio: **uma mensagem duplicada** ou **uma mensagem que não saiu**.
Se optar por reenviar, faça com uma chave nova e **saiba que está aceitando o risco de duplicata** —
não é o gateway destravando por você.

> ⚠️ **O passo 1 exige `callback_url` cadastrada.** Numa instância **só de saída** (callback vazia)
> não existe sinal nenhum a observar, e você cai direto no passo 2. Se essa ambiguidade for cara para
> você, cadastre uma `callback_url` — mesmo que ela só grave os eventos de `status` e descarte o
> resto: é o que transforma "não sei" em "sei".

> ℹ️ **`GET /v1/estado` NÃO responde a esta pergunta, e vale dizer para você não perder tempo lá.**
> O contador `enviadas` só sobe em envio bem-sucedido; um desfecho `desconhecido` conta em
> `falhas_de_envio` mesmo que a mensagem tenha saído. Os contadores medem o que o **gateway** soube,
> não o que a Meta fez.

Do lado de quem opera, esse caso grava um `ALARME` com o par `(consumidor, chave)` no log do serviço.
Isso é instrumento **deles**, não uma promessa de que alguém vai te procurar.

**E o prazo que decide se você chega a ver a resposta é o da instância** (`timeout_ms`, gravado por
número, default 5000 ms), não o do seu cliente HTTP. Se você desistir da requisição antes de o gateway
terminar, **o envio continua** — o seu cancelamento não aborta a chamada à Meta nem retém a chave, e
você fica sem saber o desfecho de um envio que aconteceu. **Dê ao seu cliente um timeout com folga
acima de 5 s** (30 s é confortável) e essa classe inteira de ambiguidade some.

---

## Marcar uma mensagem recebida como LIDA — `POST /v1/leituras` (2026-07-28)

**`POST https://zapgw.exemplo.com.br:8443/v1/leituras`** · `Authorization: Bearer <seu token>`
· **sem** `Idempotency-Key`

Saber que o cliente leu o que **você** mandou já funcionava: é o `status: "read"` que chega no
webhook. Dizer ao cliente que **você** leu a mensagem **dele** não existia — ele mandava e via dois
tiques cinzas para sempre, mesmo com o operador já trabalhando no pedido. É o que produz o
*"oi, chegou?"*.

**A causa raiz não é regressão do gateway, é consequência da migração, e todo consumidor que vier do
Baileys vai bater nela:** lá o marcador de leitura saía **sozinho** ao receber a mensagem. Na Cloud
API ele exige uma chamada explícita, e sem ela ninguém marca nada.

Mesmas regras do `/v1/messages`: rota da **LAN**, no entrypoint interno (`:8443`), e **só as
instâncias vinculadas a você respondem** — `403` para as outras, com o mesmo corpo de erro. **Não há
modelo de autorização novo:** é o mesmo vínculo consumidor↔instância.

```jsonc
// pedido
{ "instancia": "lojinha",
  "wamid": "wamid…",     // o wamid da mensagem que VOCÊ RECEBEU
  "digitando": true }    // OPCIONAL — liga o indicador de "digitando…" na mesma chamada

// resposta 200
{ "ok": true }
```

**`digitando` é opcional e por padrão ausente/`false`.** ⚠️ **Duas restrições que você descobre
errado se não ler aqui:** (a) exige o `wamid` de uma mensagem **que você recebeu** — não existe
"digitando" solto, fora de resposta a algo; (b) o indicador **cai sozinho em 25 segundos**, ou
quando você responde, o que vier primeiro — não há como mantê-lo ligado além disso, e quem esperar
que ele fique aceso vai achar que o gateway falhou.

### Por que `digitando` é um campo aqui, e não uma rota `/v1/digitando`

A Cloud API **não tem endpoint próprio** para o indicador de "digitando…": ela funde os dois no
**mesmo** `POST` do recibo de leitura, só acrescentando `typing_indicator: {"type":"text"}` ao
corpo de sempre (fonte: `developers.facebook.com/docs/whatsapp/cloud-api/typing-indicators`, lida
2026-08-20). Uma rota separada faria o gateway emitir **dois** `POST`s para o que a Meta faz em
um, e obrigaria você a escolher entre "marcar lida" e "marcar lida e digitando" sem que essa
diferença exista do outro lado.

**A resposta não tem `wa_message_id`, e isso é deliberado:** marcar como lida **não cria mensagem
nenhuma**. Inventar um id para "manter a forma" do envio seria mentir no contrato para poupar uma
linha de documentação.

### Por que é rota própria, e não um oitavo `tipo` do `POST /v1/messages`

Os sete tipos de envio têm `para`, têm conteúdo e devolvem `wa_message_id`. Marcar como lida **não
tem nenhum dos três**: o alvo é uma *mensagem* e não uma pessoa, não há corpo, e nada nasce. Enfiar
isso no envelope de envio faria três campos do contrato virarem *"opcional dependendo do tipo"* — a
mesma ambiguidade que já custou caro no par `botoes`/`botoes_template`. *(O argumento é do
de um consumidor, e foi adotado por estar certo.)*

### Repetir é seguro — e é por isso que **não** existe `Idempotency-Key` aqui

**Marcar a mesma mensagem duas vezes é inofensivo:** não nasce mensagem, não há cobrança, e o estado
final é o mesmo. A chave de idempotência existe no `/v1/messages` porque lá um retry vira **duas
mensagens no celular do seu cliente** — aqui não há duplicata possível para impedir.

Então fica escrito, para ninguém construir o controle "por precaução" daqui a seis meses: **você não
precisa guardar estado de "já marquei".** O operador abre a mesma conversa dez vezes por dia; mande
as dez. Um controle de "já marquei" do seu lado só teria custo (uma tabela, uma chave retida, um
`409` para destravar) e não compraria defeito nenhum que exista.

### Um `wamid` por chamada — não há lista, e a razão não é preguiça

**O argumento é falha parcial.** Se a rota aceitasse uma lista e 5 de 13 falhassem, o gateway teria
de inventar uma resposta para "sucesso parcial", e **toda** resposta possível é ruim: `200` mentindo,
`500` mandando você repetir o que já funcionou, ou um corpo com resultado por item — que é uma API
nova dentro da rota. Com um alvo por chamada, **falha parcial não existe como conceito**: cada
chamada é um fato.

Aceitar lista depois é aditivo; tirar a lista depois seria quebra — por isso o desenho começa
estreito.

**E o laço que você vai escrever é curto, não longo:** pela garantia da seção seguinte, marcar a
mensagem **mais recente** de uma conversa marca as anteriores dela junto. Então o volume real é *uma
chamada por conversa aberta*, não uma por mensagem — é a diferença entre 1 e 13 no caso medido logo
abaixo.

### O que a Meta faz com as mensagens ANTERIORES da conversa

**Marcar uma mensagem como lida marca também as anteriores daquela conversa.** É afirmação da
documentação oficial, lida em 2026-07-28 nas duas páginas que descrevem a chamada:

> *"When you mark a message as read, the API also marks earlier messages in the conversation as
> read."*
> — `developers.facebook.com/docs/whatsapp/cloud-api/guides/mark-message-as-read` e
> `developers.facebook.com/documentation/business-messaging/whatsapp/messages/mark-message-as-read`

**A consequência prática é grande para você:** ao abrir uma conversa com várias mensagens não lidas,
**uma chamada com o `wamid` da MAIS RECENTE basta** — você não precisa iterar. Um consumidor mediu
que 47% dos blocos de mensagens de entrada seguidas têm mais de uma mensagem (o maior tinha 13), e é
justamente esse caso que a garantia acima resolve com uma chamada só.

**Duas ressalvas honestas sobre o alcance dessa leitura:**

- as duas páginas falam de *"the conversation"* sem distinguir conversa individual de **grupo** — o
  comportamento em grupo **não foi conferido**, e este gateway não o afirma;
- a mesma fonte diz *"Mark incoming messages as read within 30 days of receipt"*. Mensagem mais
  velha que isso tende a ser recusada (`400 permanente`), e não há nada a fazer além de desistir
  daquela marcação.

### Erros

O mesmo corpo de erro e a mesma taxonomia do envio — **decida pela `classe`, nunca pelo status**:

| Status | Classe | Quando acontece |
|---|---|---|
| `400` | `permanente` | falta `instancia` ou `wamid`, o corpo não é JSON; **ou** a Meta recusou o `wamid` (ela devolve `codigo_meta` `131009` para *"Parameter value is not valid"*, o caso do wamid inválido ou velho demais) |
| `400` | `retentavel` | o corpo não chegou inteiro (sua conexão caiu no meio) |
| `401` | `config` | seu `Authorization` está ausente ou é inválido |
| `403` | `config` | a instância pedida não é sua |
| `404` | `config` | a instância pedida não existe |
| `413` | `permanente` | o corpo passou do limite de tamanho aceito (**1 MiB** por default — ver *Limites conhecidos*) |
| `502` | `config` | a credencial que o **gateway** guarda para essa instância foi recusada pela Meta, ou o `phone_number_id` cadastrado está inválido. Não é o seu token; precisa de admin |
| `502` | `desconhecido` | o gateway não obteve resposta utilizável da Meta (transporte, prazo da instância estourado) |
| `503` | `retentavel` | a instância está pausada, o gateway não falou com o próprio armazenamento, **ou** a Meta devolveu erro retentável (5xx, timeout, ou throttling reconhecido pelo **código** da Meta — não pelo status; ver a nota em *POST /v1/messages → Erros*) |

⚠️ **O `desconhecido` (`502`) aqui manda o CONTRÁRIO do que manda no envio, e é a diferença que mais
importa nesta página.** No `/v1/messages`, "não sei se a Meta criou a mensagem" significa *não
reenvie* — um retry às cegas duplicaria uma mensagem real. Aqui **não há duplicata possível**: se a
marcação pode não ter acontecido, **repita**. A própria mensagem de erro diz isso, com essas
palavras. Aplicar aqui a regra do envio deixaria a conversa com dois tiques cinzas por medo de um
dano que não existe.

### Os números desta rota aparecem em `GET /v1/estado`, com chave PRÓPRIA

Duas chaves novas no vocabulário: **`leituras_marcadas`** e **`falhas_de_leitura`** (falha ao
*marcar* leitura, nunca "falha ao ler alguma coisa"). Elas chegam sozinhas na sua leitura de
`contadores`, pela promessa da seção *Uma promessa nossa sobre o vocabulário*.

🔴 **Elas NÃO entram em `enviadas`, e isso é garantia, não detalhe de implementação.** Marcar como
lida não produz conversa e a Meta não cobra por ela. Se essa contagem caísse em `enviadas`, a sua
projeção de custo (volume × tarifa do rate card) passaria a incluir dez "envios" por conversa que o
operador abriu dez vezes — **um número inflado com cara de medição**. Quem quiser um total soma as
colunas na tela; quem só tivesse o total não conseguiria separar de volta.

---

## Bloquear e desbloquear usuários — `POST /v1/bloqueios`, `DELETE /v1/bloqueios`, `GET /v1/bloqueios` (2026-08-20)

**`POST/DELETE/GET https://zapgw.exemplo.com.br:8443/v1/bloqueios`** · `Authorization: Bearer <seu token>`

A Cloud API tem um endpoint próprio para o seu negócio parar de RECEBER de um número — esta é a
rota que o alcança. Mesmas regras das outras rotas de instância: rota da **LAN**, e só as
instâncias vinculadas a você respondem (`403` para as outras). Exclusiva de WhatsApp — ver *Rotas
que recusam `400` numa instância de Instagram*, mais abaixo.

### Bloquear — `POST /v1/bloqueios`

```jsonc
// pedido
{ "instancia": "lojinha",
  "telefones": ["5511999990000", "5511999990001"] }   // ate 1.000 por chamada

// resposta 200
{ "instancia": "lojinha",
  "operacao": "bloquear",
  "processados": [ {"telefone": "5511999990000", "wa_id": "5511999990000"} ],
  "falhas": [
    { "telefone": "5511999990001", "wa_id": "5511999990001",
      "codigo_meta": 139001, "mensagem": "…", "detalhe_meta": "…" }
  ] }
```

🔴 **A restrição que muda o desenho, com todas as letras:** só é possível bloquear quem mandou
mensagem para você nas **ÚLTIMAS 24 HORAS**. Bloqueio é **REATIVO**, nunca preventivo — não existe
pré-bloquear um número antes de ele escrever. Também não é possível bloquear outra conta business.
**As duas restrições são DA META, não do gateway**: ele não confere a janela por conta própria —
quem decide, POR NÚMERO, é ela, e o resultado chega em `falhas[]`, no formato do parágrafo
seguinte.

🔴 **Sucesso PARCIAL não é caso raro nesta rota, e o corpo é desenhado para ele:** a Meta responde
`200` no ENVELOPE e reporta erro POR NÚMERO dentro dele — 1.000 números podem virar 998 bloqueados
e 2 recusados, todos debaixo do mesmo `200`. Por isso esta rota **nunca** devolve um sucesso liso:
toda chamada volta com `processados` **e** `falhas` juntos, mesmo quando um dos dois vem vazio.
**Confira `falhas` em toda chamada** — o status `200` sozinho não prova que todos os números foram
processados.

### Desbloquear — `DELETE /v1/bloqueios`

**MESMO corpo** do `POST`, **MESMO formato** de resposta — só o método muda, e `"operacao"` vem
`"desbloquear"`.

### Listar quem está bloqueado — `GET /v1/bloqueios`

```
GET /v1/bloqueios?instancia=lojinha&limit=100&after=<cursor>&before=<cursor>
```

`limit`, `after` e `before` são OPCIONAIS e repassados direto para a paginação da Meta.

```jsonc
// resposta 200
{ "instancia": "lojinha",
  "total": 1,
  "bloqueados": [ {"wa_id": "5511999990000"} ],
  "cursor_antes": "…",
  "cursor_depois": "…" }
```

A Meta não devolve o telefone em claro nesta listagem — só o `wa_id`. `cursor_antes`/
`cursor_depois` só aparecem quando a Meta os mandou; use-os em `before`/`after` da próxima
chamada para paginar.

🔑 **Para que esta rota serve de verdade: para DISCORDAR de você.** Medido pelo `consumer-b`
em 2026-08-20, e a lição é deles: o banco deles dizia "bloqueado" e este `GET` respondeu
`{"total":0,"bloqueados":[]}`. **Duas fontes discordando em quinze segundos** transformaram um
*"acho que não funcionou"* em causa raiz — era um `Enter` no formulário deles submetendo sem
`submitter`, e portanto sem o campo que escolhia entre bloquear aqui e bloquear na Meta.

*Por que isso vale estar escrito no contrato e não só no changelog:* eles tinham catalogado esta
rota como *"auditoria, não render"* — útil, mas de baixa prioridade. **O valor dela não é listar;
é ser uma segunda fonte que pode contradizer a sua.** Bloqueio é exatamente o caso em que sucesso e
fracasso têm o mesmo sintoma: silêncio. Ninguém investiga alguém que parou de escrever.

### Limites

- **1.000 telefones por chamada** de `POST`/`DELETE` — acima disso o gateway recusa na ENTRADA
  (`400 permanente`), dizendo quantos vieram e o máximo aceito. A Meta nem chega a ser chamada.
- **64.000 usuários bloqueados no total, por conta** — limite DA META, não espelhado aqui: quem
  sabe o total é ela, e o erro chega junto do número que o estourou (`falhas[]`) ou, se a chamada
  inteira for recusada por isso, em `erro.detalhe_meta`.
  ⚠️ **Nunca vimos esse erro acontecer** (conferido em 2026-08-20: nenhuma ocorrência em código,
  teste ou registro nosso). Então **não sabemos o `codigo_meta` dele** — e não vamos inventar um.
  Quem bater primeiro, mande o código por este canal e ele entra aqui.
- Telefone é **CANONIZADO** como no envio — mandar sem o nono dígito não é erro, mas também não
  bloqueia o número que você pensa: o gateway insere o dígito antes de falar com a Meta.

### Erros

| Status | Classe | Quando acontece |
|---|---|---|
| `400` | `permanente` | falta `instancia`, `telefones` ausente/vazio, acima de 1.000 telefones, ou o corpo não é JSON |
| `400` | `retentavel` | o corpo não chegou inteiro (sua conexão caiu no meio) |
| `401` | `config` | seu `Authorization` está ausente ou é inválido |
| `403` | `config` | a instância pedida não é sua |
| `404` | `config` | a instância pedida não existe |
| `503` | `retentavel` | a instância está pausada, ou o gateway não falou com o próprio armazenamento |
| `502` | `config` | a credencial que o **gateway** guarda para essa instância foi recusada pela Meta, ou o `phone_number_id` cadastrado está inválido |
| `502` | `desconhecido` | o gateway não obteve resposta utilizável da Meta para a **CHAMADA INTEIRA** — nenhum número foi processado; repetir é seguro (bloquear/desbloquear não tem efeito colateral por si só) |

⚠️ **Um `200` nunca é "erro" nesta rota — mesmo que TODOS os números tenham ido parar em
`falhas[]`.** O envelope respondeu; o veredito por número está no corpo. A tabela acima descreve
só a falha da CHAMADA INTEIRA (nenhum número processado) — não confunda com o sucesso parcial do
parágrafo anterior.

---

## Saber se um canal ainda está apto a enviar

**`GET /v1/instances/{slug}/health`** · `Authorization: Bearer <seu token>`

Mesmas regras do `/v1/messages`: rota da **LAN**, no entrypoint interno (`:8443`), e só as instâncias
vinculadas a você respondem — `403` para as outras, sem que o gateway chegue a falar com a Meta.

Ele existe porque o `/v1/health` **não responde a esta pergunta**: aquele `200` diz que o processo do
gateway está de pé, e sai igualzinho com **todos** os tokens revogados. Token revogado pelo cliente
na Meta é a falha que morre em silêncio — nada muda no gateway, e o primeiro a saber seria o seu
cliente final que não recebeu. Este endpoint pergunta à Meta, com o token daquela instância, se ela
ainda o aceita (`GET /{phone_number_id}`, que não cria nada do outro lado).

> **`GET /v1/health` responde uma pergunta diferente e mais simples: que binário é este?**
> A **forma** é `{"ok": true, "versao": "<a versão do binário no ar>"}` — por exemplo
> `{"ok":true,"versao":"0.30.0"}`. **O número do exemplo é ilustrativo e envelhece**; o que o contrato
> garante são as duas chaves, não o valor. `ok` continua existindo e continua `true` sempre (é a
> garantia pública deste gateway; o formato só cresce, então quem só lê `ok` não quebra). `versao` é a
> identidade do binário, injetada em tempo de **build** (nunca lida de disco em produção); sem
> injeção o valor é `"desenvolvimento"`, nunca um número plausível. Antes disso a única forma de
> saber o que estava rodando era comparar sha256 do binário ou acreditar na tag — e a tag pode
> divergir do que está no ar (aconteceu em 2026-07-25, ver a nota de MUDANÇA QUE QUEBRA mais abaixo).

Apto a enviar → `200`:

```jsonc
{ "ok": true,
  "numero_exibido": "5511999990000",   // vem do cadastro do gateway, não da resposta da Meta
  "verificado_em": "2026-07-25T12:00:00Z" }
```

Qualquer outro desfecho → **`503`**, com o mesmo corpo de erro do envio
(`{"erro":{"classe":…,"codigo_meta":…,"mensagem":…}}`). O status é sempre `503` de propósito: um probe
responde **uma** pergunta — este canal está apto a enviar agora? —, e obrigar o seu monitor a aprender
a tabela de status inteira só para decidir "vermelho ou verde" seria transformar um sinal em
interpretação. **É a `classe` que diz o que fazer:** `config` (token recusado, `phone_number_id`
inválido) é *chame gente, isso não se conserta sozinho*; `retentavel` é *espere* (instância pausada,
`5xx`, timeout, ou throttling da Meta reconhecido pelo **código** — não pelo status; ver a nota em
*POST /v1/messages → Erros*); `desconhecido` é *o gateway não conseguiu falar com a Meta, então
não sabe*.

**Não há cache, e isso é a funcionalidade.** Toda chamada fala com a Meta. Um probe com cache mente
por exatamente o tempo do cache — que é a janela em que ele mais importa —, e a falha que ele existe
para acusar voltaria a ser silenciosa. Em troca, **a frequência é sua**: cada chamada custa uma ida à
Graph API na conta daquela instância, então não o coloque num laço apertado. O `verificado_em` viaja
justamente para que ninguém no meio do caminho possa reapresentar uma resposta velha como nova.

O que ele **não** prova: que a mensagem chega no celular de alguém. Token aceito não é mensagem
entregue — para isso existe o **`POST /v1/fumaca`**, que manda uma mensagem de verdade para um número
que você escolhe.

---

## Duas perguntas diferentes — e a segunda tem fonte PRÓPRIA, que sobrevive à sua queda (2026-08-06, atualizado 2026-08-07)

Quando um consumidor quer "saber o status", quase sempre são **duas** perguntas, e elas têm fontes
diferentes. Confundir as duas é como um dia inteiro de trabalho deste projeto foi gasto.

| Sua pergunta | Onde responder | Por quê |
|---|---|---|
| **"como o gateway está?"** — quantas mensagens, token válido, quando chegou a última | `GET /v1/estado`, logo abaixo | o gateway sabe isso, e sabe honestamente |
| **"vocês estão me alcançando?"** | **fonte externa**, nesta seção — e um espelho de conveniência dela em `GET /v1/estado` (`alcance_externo`, 2026-08-07) | 🔴 o gateway **não pode medir isto sozinho**; ele só pode REPETIR o que uma sonda que roda fora da nossa rede já mediu |

🔴 **O ESPELHO NÃO SUBSTITUI A FONTE EXTERNA, e isso é decisão, não lacuna.** O caso em que a sonda
mais importa é o **gateway calado** — e é exatamente aí que perguntar ao gateway não devolve nada.
Um status que compartilha o domínio de falha do que ele monitora não é status. Use `alcance_externo`
quando `GET /v1/estado` já está na sua tela (evita uma segunda chamada); use a URL desta seção quando
suspeitar que o gateway inteiro está fora — ela é a única das duas que sobrevive à nossa queda.

### Por que o gateway não pode MEDIR isto sozinho

**Requisição que não chega não deixa rastro.** Se o caminho público cair, o processo continua vivo,
o `GET /v1/health` continua `200`, os contadores apenas param de subir e nada no journal registra
uma entrega que nunca aconteceu. Foi medido em 2026-08-06: o link caiu, e **os quatro instrumentos
internos ficaram verdes durante a queda inteira**.

E há uma razão ainda mais simples: **qualquer resposta servida pelo gateway está indisponível
exatamente quando a resposta seria "não".** Um `alcancavel: true` calculado por dentro seria verdade
sempre que você conseguisse lê-lo — o que o torna informação nenhuma. Por isso `alcance_externo`
nunca é medição própria: é o gateway **perguntando à mesma sonda externa que você poderia perguntar
direto** e devolvendo o que ela respondeu, pela mesma disciplina de "ninguém fala direto com a Meta"
aplicada aqui a um terceiro que não é a Meta — pedido explícito do dono, para que você só precise
falar com o gateway quando ele está de pé.

### A fonte externa

Uma sonda roda **fora da nossa rede**, a cada 5 minutos, e faz exatamente o que a Meta faz: um `GET`
no caminho público de entrada, conferindo **o corpo da resposta**, não só o status. O veredito dela é
público e **não exige autenticação**:

```
GET https://healthchecks.io/b/2/c6f700a6-1982-408d-a2c0-d2f959c38da6.json
    → {"status": "up", "total": 1, "grace": 0, "down": 0}      (medido em 2026-08-13 17:50 -03)
```

**Leia `status`, e SÓ ele.** `total`, `grace` e `down` são contadores internos do badge (quantos
checks ele cobre, quantos em tolerância, quantos caídos) — este badge cobre **1** check, e derivar
qualquer coisa deles é acoplar-se a detalhe de implementação de um terceiro. *Esta linha existe
porque o exemplo aqui já mostrou só `{"status": "up"}`, abreviado — e exemplo abreviado que parece
completo é o jeito de alguém escrever um parser por igualdade exata.*

| Valor | O que significa |
|---|---|
| `up` | nos últimos minutos, uma máquina fora da nossa rede pediu ao caminho público **e o gateway respondeu** |
| `down` | **ou** o caminho caiu, **ou** a própria sonda parou de medir |
| qualquer outro literal | **"não consegui verificar"** — nunca verde, nunca queda — **e avise-nos**: o vocabulário cresceu e este contrato precisa mudar |

**`status` é string aberta, não enum.** Só medimos `up` e `down` até hoje; tratar o campo como
vocabulário fechado devolveria algo plausível para um valor que ninguém conferiu — a mesma razão pela
qual `alcance_externo.veredito` viaja **sem tradução**.

⚠️ **`GET`, e só nesta URL.** O healthchecks tem um segundo endereço, o de **ping** (escrita), que é o
que a sonda usa para dizer "medi e está de pé". Ele é secreto, mora só aqui, e **não se deriva do UUID
do badge**. Um `POST` vindo de fora naquele endereço falsificaria a medição: o monitor ficaria verde
sem ninguém ter medido nada.

🔴 **A fonte externa NÃO tem um terceiro estado, e não vai ter.** "Não consegui verificar" é o
desfecho da SUA LEITURA — não alcancei, HTTP ≠ 200, corpo que não é JSON, JSON sem `status`, literal
desconhecido —, **nunca** um veredito que ela devolva. Publicar "eu não consegui medir" como estado
que o consumidor trata como não-queda faria a **morte da sonda** virar não-alarme, que é o buraco que
ela existe para fechar. É o mesmo tratamento que o gateway dá a esta URL
(`internal/outbound/external_probe.go:286-316`): nenhuma dessas falhas inventa um veredito.

**O corpo não traz carimbo de tempo** — não dá para calcular idade a partir dele, e não é preciso:
dado velho aqui não fica verde calado, **vira `down` sozinho** depois de período + tolerância. É a
diferença entre um dead-man switch e um cache: o cache serve o último verde para sempre, este apodrece
para vermelho.

🔴 **`down` NÃO distingue as duas causas, e isso é deliberado.** A regra é *"sem sinal positivo,
considere parado"* — porque monitor que morre em silêncio é indistinguível de "está tudo bem", e é
exatamente esse o defeito que esta sonda existe para fechar. **As duas causas exigem gente; nenhuma
das duas é "está tudo bem".**

⚠️ **Três limites, para você não tirar conclusão a mais:**

- **É por GATEWAY, não por instância.** Ele diz que o caminho de entrada está de pé — não diz nada
  sobre a *sua* instância, seu token ou sua cota. Isso é o `GET /v1/estado`.
- **`down` demora até ~20 min para aparecer** (5 min de período + 15 de tolerância). Ele não serve
  para detectar soluço de segundos, e não precisa: a Meta reenfileira por **36 h**.
- **Não substitui a sua própria detecção de silêncio.** Você sabe qual é o seu volume normal; nós
  não.


## Ler os números desta instância — `GET /v1/estado` (2026-07-28)

**`GET /v1/estado?instancia={slug}`** · `Authorization: Bearer <seu token>`
**Parâmetro opcional:** `&serie_dias={1..90}` — o tamanho da série diária (default 7); ver a seção
*`serie_diaria` + `?serie_dias=`*, abaixo.

Mesmas regras do `/v1/messages`: rota da **LAN**, no entrypoint interno (`:8443`), e **só as
instâncias vinculadas a você respondem** — `403` para as outras, com o mesmo corpo de erro do envio.
Não há modelo de autorização novo aqui: é o mesmo vínculo consumidor↔instância.

**Ela existe para você ALARMAR, não para você desenhar.** O gateway não tem painel e não vai ter; o
número que ele promete sai por aqui, e quem desenha é quem já tem web. O ganho real: você já tem
sistema de aviso — com estes campos, `alarme_perda_definitiva.ultimos_7_dias > 0` vira alerta
automático **num sistema que já sabe alertar**, em vez de um número que só quem tem acesso ao servidor
consegue ver.

**São os MESMOS números que quem opera o gateway vê na tela dele**, lidos da mesma função no mesmo
instante — não uma segunda conta que concorda por coincidência. As chaves de `contadores` saem do vocabulário fechado do
gateway; quando uma chave nova nascer, ela aparece aqui **sozinha**, sem release de contrato. Trate
chave desconhecida como número, nunca como erro.

**A leitura NUNCA fala com a Meta.** Pode chamar na frequência que o seu painel quiser: tudo aqui sai
do banco e de um cache que o gateway atualiza no ritmo dele. Consequência que vale saber: a Meta fora
do ar **não** derruba esta rota — ela aparece no `token_meta`, que é onde deve aparecer.

### Exemplo executado

Capturado rodando o handler de verdade (não digitado): instância ativa, 13 recebidas e 13 entregues
na semana (4 delas hoje), 2 enviadas, 1 falha de envio, 3 leituras marcadas, o veredito do token
medido no instante da captura e o certificado do callback observado 37 minutos antes. `versao` aqui é
o valor que a suíte injeta; em produção é o **mesmo** valor do `GET /v1/health`. As séries foram
encurtadas com `…` **só na colagem**, para caber — a resposta traz sempre a janela inteira, e todos os
dias com todas as chaves.

*(Recapturado em 2026-07-29, primeiro quando os campos `carimbos_desde` e `serie_7_dias[].dia_utc`
entraram, de novo quando entraram as chaves `leituras_marcadas` e `falhas_de_leitura`, e de novo
quando `serie_diaria` entrou — os VALORES saem do handler, nunca digitados.
**Uma exceção, escrita para ninguém confundir com o resto:** as sete chaves `cobranca_*`
**não foram recapturadas** — elas entraram nesta colagem em zero, porque o cenário capturado não
tinha nenhum `sent` com cobrança e zero é exatamente o que a garantia acima produz nesse caso (toda
chave do vocabulário aparece, mesmo sem evento). Zero aqui é o preenchimento garantido, não um número
inventado.
**`carimbos_desde` aparece aqui igual ao `gerado_em` porque a instância desta captura acabou de ser
criada**; numa instância que já existia ele é o instante em que a migração rodou, e numa criada mês
passado é o nascimento dela.
**SEGUNDA EXCEÇÃO (T-098, 2026-07-30):** o bloco `token_instagram` foi acrescentado À MÃO a esta
colagem — o cenário original é WhatsApp e não tinha esse campo quando foi capturado. Os SETE valores
(`nao_se_aplica` e os seis `null`) são exatamente o que `TestGETStateWhatsappInstagramTokenIsNotApplicableInTheJSON`
(`internal/outbound/state_instagram_test.go`) prova contra o handler de verdade — a garantia é
mecânica, só a colagem aqui é manual.
**TERCEIRA EXCEÇÃO (T-107, 2026-07-30):** os campos `tipo` e `ig_id` também foram acrescentados À MÃO
— o cenário original é anterior aos dois. Os valores (`"whatsapp"` e `"nao_se_aplica"`) são exatamente
o que `TestGETStateWhatsappExposesTypeAndIgIDAsNotApplicableInTheJSON` (`internal/outbound/state_instagram_test.go`)
prova contra o handler de verdade.)*

```jsonc
{
  "instancia": "lojinha",
  "tipo": "whatsapp",
  "ig_id": "nao_se_aplica",
  "estado": "ativa",
  "pausada": false,
  "versao": "9.9.9-teste",
  "gerado_em": "2026-07-29T00:00:02Z",
  "carimbos_desde": "2026-07-29T00:00:02Z",
  "contadores": {
    "alarme_perda_definitiva":   { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_ausente":          { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_authentication":   { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_cobravel":         { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_marketing":        { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_outra":            { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_service":          { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "cobranca_utility":          { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "conta_descartada":          { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "entregues":                 { "hoje": 4, "ultimos_7_dias": 13, "ultimo_em": "2026-07-29T00:00:02Z" },
    "enviadas":                  { "hoje": 2, "ultimos_7_dias": 2,  "ultimo_em": "2026-07-29T00:00:02Z" },
    "falhas_de_envio":           { "hoje": 1, "ultimos_7_dias": 1,  "ultimo_em": "2026-07-29T00:00:02Z" },
    "falhas_de_leitura":         { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "leituras_marcadas":         { "hoje": 3, "ultimos_7_dias": 3,  "ultimo_em": "2026-07-29T00:00:02Z" },
    "numero_descartado":         { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null },
    "recebidas":                 { "hoje": 4, "ultimos_7_dias": 13, "ultimo_em": "2026-07-29T00:00:02Z" },
    "recusadas_pelo_consumidor": { "hoje": 0, "ultimos_7_dias": 0,  "ultimo_em": null }
  },
  "serie_7_dias": [
    { "dia": "2026-07-23", "dia_utc": "2026-07-23", "contadores": { "alarme_perda_definitiva": 0, /* as 7 chaves cobranca_* vêm aqui, todas 0 nesta captura — omitidas só nesta colagem */ "conta_descartada": 0, "entregues": 0, "enviadas": 0, "falhas_de_envio": 0, "falhas_de_leitura": 0, "leituras_marcadas": 0, "numero_descartado": 0, "recebidas": 0, "recusadas_pelo_consumidor": 0 } },
    // … 2026-07-24 a 2026-07-27, todos zerados nesta captura …
    { "dia": "2026-07-28", "dia_utc": "2026-07-28", "contadores": { "alarme_perda_definitiva": 0, /* as 7 chaves cobranca_* vêm aqui, todas 0 nesta captura — omitidas só nesta colagem */ "conta_descartada": 0, "entregues": 9, "enviadas": 0, "falhas_de_envio": 0, "falhas_de_leitura": 0, "leituras_marcadas": 0, "numero_descartado": 0, "recebidas": 9, "recusadas_pelo_consumidor": 0 } },
    { "dia": "2026-07-29", "dia_utc": "2026-07-29", "contadores": { "alarme_perda_definitiva": 0, /* as 7 chaves cobranca_* vêm aqui, todas 0 nesta captura — omitidas só nesta colagem */ "conta_descartada": 0, "entregues": 4, "enviadas": 2, "falhas_de_envio": 1, "falhas_de_leitura": 0, "leituras_marcadas": 3, "numero_descartado": 0, "recebidas": 4, "recusadas_pelo_consumidor": 0 } }
  ],
  // Sem `?serie_dias=`, esta é a MESMA janela de 7 dias acima, dia a dia igual.
  // Com `?serie_dias=30`, ela tem 30 entradas e `serie_7_dias` continua com 7.
  "serie_diaria": [
    { "dia": "2026-07-23", "dia_utc": "2026-07-23", "contadores": { "alarme_perda_definitiva": 0, /* as 7 chaves cobranca_* vêm aqui, todas 0 nesta captura — omitidas só nesta colagem */ "conta_descartada": 0, "entregues": 0, "enviadas": 0, "falhas_de_envio": 0, "falhas_de_leitura": 0, "leituras_marcadas": 0, "numero_descartado": 0, "recebidas": 0, "recusadas_pelo_consumidor": 0 } },
    // … 2026-07-24 a 2026-07-27, todos zerados nesta captura …
    { "dia": "2026-07-28", "dia_utc": "2026-07-28", "contadores": { "alarme_perda_definitiva": 0, /* as 7 chaves cobranca_* vêm aqui, todas 0 nesta captura — omitidas só nesta colagem */ "conta_descartada": 0, "entregues": 9, "enviadas": 0, "falhas_de_envio": 0, "falhas_de_leitura": 0, "leituras_marcadas": 0, "numero_descartado": 0, "recebidas": 9, "recusadas_pelo_consumidor": 0 } },
    { "dia": "2026-07-29", "dia_utc": "2026-07-29", "contadores": { "alarme_perda_definitiva": 0, /* as 7 chaves cobranca_* vêm aqui, todas 0 nesta captura — omitidas só nesta colagem */ "conta_descartada": 0, "entregues": 4, "enviadas": 2, "falhas_de_envio": 1, "falhas_de_leitura": 0, "leituras_marcadas": 3, "numero_descartado": 0, "recebidas": 4, "recusadas_pelo_consumidor": 0 } }
  ],
  "token_meta": {
    "veredito": "ok",
    "medido_em": "2026-07-29T00:00:02Z",
    "conferido_em": "2026-07-29T00:00:02Z",
    "checagem_falhando_desde": null
  },
  "certificado_do_callback": {
    "estado": "observado",
    "expira_em": "2026-10-21T00:00:02Z",
    "observado_em": "2026-07-28T23:23:02Z"
  },
  "numero_na_meta": {
    "qualidade": {
      "estado": "observado",
      "valor": "GREEN",
      "observado_em": "2026-07-29T00:00:02Z",
      "fonte": "medicao"
    },
    "limite_de_mensagens": {
      "estado": "observado",
      "valor": "TIER_1K",
      "observado_em": "2026-07-29T00:00:02Z",
      "fonte": "medicao"
    },
    "conferido_em": "2026-07-29T00:00:02Z"
  },
  "token_instagram": {
    "veredito": "nao_se_aplica",
    "definido_em": null,
    "expira_em": null,
    "dias_restantes": null,
    "renovado_em": null,
    "falhando_desde": null,
    "instrucao": null
  },
  "entrada": {
    "via": "tunel",
    "conector": {
      "estado": "observado",
      "conexoes_prontas": 4,
      "medido_em": "2026-07-29T00:00:02Z",
      "falhando_desde": null
    },
    "ultimo_webhook_em": "2026-07-29T00:00:02Z"
  }
}
```

*(`token_instagram` sai `nao_se_aplica` nesta captura porque `lojinha` é WhatsApp — ver a seção
própria do bloco, mais abaixo, para o exemplo numa instância Instagram.)*

*(**QUARTA EXCEÇÃO (T-120, 2026-08-06):** o bloco `entrada` foi acrescentado À MÃO a esta colagem — o
cenário original é anterior a ele. A forma e os estados são exatamente os que
`internal/outbound/ingress_test.go` prova contra o handler de verdade; os valores mostrados são os de
uma instalação que entra por túnel com o conector respondendo.)*

### `ultimo_em` — o campo que responde o que o contador não responde

**Contador parado é ambíguo entre "falhou" e "ninguém escreveu" — os dois são o mesmo número.**
Isso cobrou caro em 2026-07-28, 11:07: a entrega parou e `recebidas` ficou parado em 4. A queda só
apareceu porque havia um monitor de log armado à mão naquela manhã.

O carimbo desfaz a ambiguidade porque ele **envelhece**. Com ele a sua regra de alarme é
*"`entregues.ultimo_em` tem mais de N minutos"* — e ela funciona **sem você saber nada do nosso
volume normal**, que é a única regra que funciona para quem não conhece o tráfego do outro.

- É o instante do **último** evento daquela chave, em UTC, RFC3339;
- `null` quer dizer **nunca aconteceu** dentro da retenção dos contadores (90 dias por padrão);
- ele **não** é cortado pela janela de 7 dias: um evento de 20 dias atrás aparece com a data dele.
  Cortar faria "velho" e "nunca" virarem a mesma coisa, que é o defeito que ele existe para curar.

Os quatro que mais interessam: `recebidas.ultimo_em`, `entregues.ultimo_em`, `enviadas.ultimo_em` e
`falhas_de_envio.ultimo_em`.

### `carimbos_desde` — desde quando esta instância carimba (2026-07-28)

**`ultimo_em: null` ainda esconde DOIS estados**, e a regra acima só descreve um deles. Sobram
*"nunca aconteceu"* (normal) e *"aconteceu **antes** de o carimbo ser gravado"* — porque o carimbo
nasceu na `v0.23.0`, e o que veio antes não deixou instante nenhum. Para um alarme são coisas
diferentes: no primeiro caso não há nada a saber; no segundo há um ponto cego.

`carimbos_desde` é o instante (UTC/RFC3339, **no topo da resposta**, fora de `contadores`) a partir do
qual **aquela instância** grava carimbo. A leitura completa passa a ser:

```
ultimo_em != null                      -> a data é a resposta
ultimo_em == null e carimbos_desde é ANTIGO  -> nunca aconteceu mesmo
ultimo_em == null e carimbos_desde é RECENTE -> pode ter acontecido antes, e não dá para saber
```

**Ele é por INSTÂNCIA e vem do banco — não é a data de uma versão nossa.** Instância criada hoje
carimba desde hoje; instância que já existia quando o campo nasceu recebeu o instante em que a
migração rodou. Esse valor pode ser **mais tarde** que a verdade (talvez ela já carimbasse há dias), e
o erro é para esse lado de propósito: ele faz você tratar como *"não sei"* uma faixa em que talvez
houvesse carimbo, nunca o contrário.

> **Por que ele existe em vez de você anotar a data da `v0.23.0`:** foi exatamente essa a alternativa,
> e ela é uma constante escrita à mão no código de cada consumidor — que **apodrece na primeira
> instância nova**, porque aquela não carimba desde a `v0.23.0`, e sim desde que nasceu. O pedido foi
> de um consumidor, e o argumento é o mesmo que já nos fez pôr **dois** carimbos no `token_meta` e no
> `certificado_do_callback`: quem lê precisa saber a idade do **instrumento**, não só a do dado.

#### 🔴 A regra acima está INCOMPLETA sem uma janela de referência

*"N minutos sem entrega"*, sozinho, **mede o sono do seu cliente, não a saúde do gateway.** Levantado
por um consumidor em 2026-07-28, algumas horas depois de subir a regra exatamente como este
documento a recomendava:

> *"Ela ia tocar hoje à noite, e todas as noites. A cliente **não escreve às 3h da manhã**."*

E há uma **segunda mordida**, que só aparece em quem conserta a primeira mal: gatear apenas por
*"estamos no horário comercial?"* faz o alarme disparar **na abertura, todo dia** — às 8h05 a última
entrega é a de ontem à noite, com 11h de idade, velha por definição e sem nada de errado.

**A regra completa tem duas condições:**

```
alarma se:  <chave>.ultimo_em está mais velho que N minutos
        E   a janela de tráfego de hoje já está aberta há pelo menos N minutos
```

Conferido com o relógio adiantado, contra produção:

```
03:05 -> ok      (madrugada: silêncio é o esperado)
08:05 -> ok      (janela recém-aberta; a entrega velha é a de ontem)
12:05 -> ALARME  "nenhuma entrega há 1330 min"
20:05 -> ok      (janela fechada)
```

A janela é **sua**, não nossa — o consumidor que levantou isto usa 8h–20h em dia útil e 10h–22h no fim de semana, a
mesma que decide envio lá. O seu caso será outro; o defeito é o mesmo.

> **Por que esta regra parece certa quando se escreve, e é o que a torna perigosa:** *"quem tem
> tráfego de máquina 24/7 não sente isso"* (palavras dele). Consumidor com tráfego **humano** só
> descobre na primeira madrugada — e alarme que toca toda noite sem motivo é como se ensina uma
> equipe a ignorar alarme, que é o oposto do que este campo existe para fazer.

### `pausada` — antes de alarmar por silêncio, olhe este campo

Instância pausada responde `503` no envio, o volume vai a zero e o carimbo envelhece — **exatamente
como uma queda**. Sem este campo o seu alarme diria *"nenhuma entrega há 200 minutos"* quando a causa
é uma pausa deliberada. **Regra: `pausada == true` suprime o alarme de silêncio** (e, se quiser,
levanta um aviso próprio, porque instância pausada por engano também é problema).
`estado` é a mesma informação em palavra (`"ativa"` / `"pausada"`), para tela de gente.

### `serie_7_dias` — o dia a dia da mesma janela

> ⚠️ **OBSOLETA desde 2026-07-29, e continua funcionando byte a byte.** A sucessora é
> `serie_diaria`, que aceita a janela que você pedir (30 dias, por exemplo) — a seção logo abaixo.
> Tudo o que está descrito aqui vale igual para as duas.

Sempre **7 entradas**, do dia mais velho para o mais novo, cada uma com **todas** as chaves do
vocabulário. Dia sem tráfego vem presente e zerado — nunca ausente: uma série de tamanho variável
faria o seu gráfico mudar de forma conforme o tráfego, e um dia zerado sumiria em vez de aparecer
como zero.

**Cada entrada traz DUAS chaves com o MESMO valor: `dia_utc` (o nome certo) e `dia` (obsoleto).**
Use `dia_utc`. A data é em **UTC**, o mesmo fuso em que os contadores são gravados. É por isso que a
soma dos sete dias bate exatamente com `ultimos_7_dias`; se você converter para o fuso local antes de
somar, ela deixa de bater — e a culpa não é do gateway.

> 🛑 **DECIDIDO EM 2026-07-31: o dia continua sendo UTC, e NÃO haverá parâmetro de fuso.** Não peça
> `?tz=`; ele não vai existir. *Isto está escrito para você tirar o item da sua fila, não para você
> esperar.*
>
> **Por que não, e o motivo é técnico, não de prioridade:** o dia é decidido na **ESCRITA**, não na
> leitura. O contador agrega em `INSERT INTO contador (slug, dia, …)` com a data já em UTC
> (`internal/config/counter.go`, a função `dayOf`). **Depois de agregado, a informação de em que dia
> LOCAL cada evento caiu não existe mais** — só sobrevive o instante do último evento do balde.
> Nenhum parâmetro de leitura recupera isso: um `?tz=` devolveria número **errado** com cara de
> certo, que é pior que não ter.
>
> **A consequência prática que você precisa saber, medida por um consumidor em 2026-07-28:** o dia
> daqui vira às **21h de Brasília**, no meio da noite de operação. Um painel que mostre "recebidas
> hoje" lendo `dia_utc` mostra `0` às 21h01 mesmo com movimento. **Se o seu "hoje" precisa ser o
> local, a conta é sua e a fonte é sua** — o gateway responde por UTC, com precisão, e não finge
> outra coisa.
>
> *Rótulo dizendo "(UTC)" ao lado de um número que o operador lê como "hoje" **não resolve** — foi a
> primeira tentativa do consumidor, e o dono dele cortou: "é óbvio que os dados têm que mostrar a
> realidade". Concordamos; por isso o gateway não vai fingir que sabe o seu fuso.*

> **Por que o nome novo, e por que ele vale o campo repetido (pedido de um consumidor).** Nós
> tínhamos "resolvido" o fuso escrevendo o aviso abaixo e pedindo que você escrevesse *"UTC"* na sua
> tela. Eles discordaram do **remédio**, não do diagnóstico: *"ele põe a guarda na intenção do
> consumidor, e integrador novo não lê aviso nenhum. Um nome de campo viaja com o dado até dentro do
> `console.log` de quem estiver depurando às duas da manhã."* E provaram com o próprio bug que
> estavam consertando naquele dia: alguém escreveu na docstring *"NÃO mexa neste campo, é o ponto
> mais fácil de errar"*, a guarda ficou no aviso, e o `default=` da coluna gravou errado assim mesmo
> — custou orçamento não entregue e dois reenvios queimados.

**`dia` é OBSOLETO e continua funcionando, byte a byte.** Renomear seria quebra de contrato, e a
janela para crescer sem quebrar era aquela — a rota tinha **um dia de vida** quando isto entrou.

**Marcado obsoleto em 2026-07-28, portanto removível a partir de 2027-01-28** (pela *Política de
depreciação*, no fim deste documento). Até lá ele sai na resposta, com o mesmo valor de
`dia_utc`. **Migre para `dia_utc` sem pressa e sem esperar aviso nenhum**: a data está escrita, e ela é o aviso.

> ⚠️ **O sintoma que isso produz na tela de quem lê em UTC−3**, levantado por um consumidor em
> 2026-07-28 ao montar o painel dele: **tudo o que sai depois das 21h locais cai no dia SEGUINTE da
> lista.** Não é erro de contagem — é a fronteira do dia em UTC —, mas quem olhar sem saber jura que
> é, e vai procurar bug de contador onde só há fuso.
> **Escreva isso na sua tela**, não só no seu código. Quem lê o painel não lê o contrato.

### `serie_diaria` + `?serie_dias=` — a janela que **você** pede (2026-07-29)

**`GET /v1/estado?instancia={slug}&serie_dias=30`**

`serie_7_dias` responde *"está entregando?"* — a pergunta operacional, e sete dias bastam para ela.
**Não responde *"quanto vou gastar no mês"***, que é um gráfico de 30 dias. Esse gráfico existia no
painel de um consumidor, alimentado pelo `analytics` da WABA, **direto na Graph**, e a regra *ninguém
fala direto com a Meta* fechou aquele caminho: esta janela é a substituta, e é **dívida que a regra
criou**, não conveniência.

- `serie_diaria` tem **exatamente** `serie_dias` entradas, do dia mais velho para o mais novo, com a
  mesma forma de `serie_7_dias` (`dia_utc`, `dia` obsoleto, e **todas** as chaves em **todo** dia,
  inclusive nos dias sem tráfego);
- **sem o parâmetro, são 7 dias** — o mesmo conteúdo de `serie_7_dias`, dia a dia. O default não
  cresceu para não inflar em treze vezes a resposta de quem nunca pediu nada;
- o dado **já estava no banco**: o gateway guarda contador diário por **90 dias** (a retenção que este
  documento sempre declarou), e o corte em 7 era da rota, nunca do armazenamento. Não houve
  armazenamento novo para isto funcionar.

#### 🔴 Há um teto, e pedir além dele é `400` — nunca uma série curta em silêncio

```jsonc
// GET /v1/estado?instancia=lojinha&serie_dias=91   -> 400
{ "erro": { "classe": "permanente",
            "mensagem": "`serie_dias` = 91, mas este gateway guarda contador por 90 dias — a serie mais longa possivel tem 90 entradas, e as mais velhas de uma janela maior sairiam zeradas sem terem sido medidas" } }
```

**O teto é a própria retenção**, e a mensagem cita o número **em vigor nesta instalação** (o operador
pode encurtá-lo pela variável `ZAPGW_TTL_CONTADORES_DIAS`). `serie_dias` que não seja
um inteiro ≥ 1 leva o mesmo `400`.

**Por que erro e não uma série encurtada**, que seria "mais gentil": os dias que a purga já apagou
voltariam **zerados**, e zero de dia purgado é **indistinguível de zero de dia sem tráfego** —
exatamente na ponta mais velha do gráfico, que é a que ninguém confere. Você somaria o mês e o total
sairia menor sem nada acusando. É a mesma regra do catálogo de templates truncado: **incompleto é
erro, nunca `200`**.

#### O segundo limite é a idade da INSTÂNCIA, e ele já está na resposta

Retenção não é a única fronteira: uma instância criada há três dias não tem trinta dias de história
para contar, e a série volta com zeros legítimos nos dias em que **ela não existia**. Quem responde
isso é o **`carimbos_desde`** no topo da mesma resposta — a idade do instrumento. A leitura é:

```
dia da série ANTERIOR a carimbos_desde  -> ausência de instrumento, não ausência de tráfego
dia da série POSTERIOR a carimbos_desde -> zero é zero de verdade
```

*Isto vale muito para quem plota custo: uma média mensal calculada sobre os dias anteriores ao
nascimento da instância sai diluída, e o número parece razoável — que é o pior jeito de errar.*

#### `serie_7_dias` continua funcionando, byte a byte, e é OBSOLETA

Ela **não muda de forma nunca**: com `serie_dias=30`, `serie_diaria` tem 30 entradas e `serie_7_dias`
continua com 7 — e as duas saem da **mesma leitura do banco**, então o sufixo de 7 dias de uma é igual
à outra, dia a dia e número a número (há teste guardando isso).

**Migre para `serie_diaria`** (com ou sem o parâmetro: sem ele são os mesmos 7 dias). O nome
`serie_7_dias` carrega um número, e por isso ele nunca poderia crescer — um campo com esse nome e 30
entradas mentiria sobre si mesmo dentro do `console.log` de quem estiver depurando às duas da manhã,
que é exatamente o argumento que produziu o `dia_utc`.

**Marcada obsoleta em 2026-07-29, portanto removível a partir de 2027-01-29** (pela *Política de
depreciação*, no fim deste documento) — a mesma forma do `dia`/`dia_utc`. Até lá ela sai na
resposta, com a mesma forma de sempre.

### Uma promessa nossa sobre o vocabulário, e o que ela exige de você

`contadores` traz **toda** chave do vocabulário, sempre, mesmo zerada — e isso é garantido por teste
do nosso lado, não por convenção. **A consequência boa:** chave nova que criarmos aparece na sua tela
sem release seu.

**A consequência que exige compromisso:** se um dia removermos uma chave, um consumidor que confie na
presença dela quebra em silêncio. Então fica escrito: **remoção de chave do vocabulário é mudança que
quebra** — ela passa pela *Política de depreciação* (abaixo) e entra em *Mudanças que quebram*, que é
o único lugar onde o aviso existe. Chave nova, não — é aditiva, e você a recebe de graça.

*(Um consumidor pediu exatamente isso em 2026-07-28, depois de travar por teste, do lado dele, que
a leitura não filtra por lista fixa: **o teste dele continua passando se a nossa garantia cair**, e
aí a tela dele mentiria. A garantia é nossa; conferi-la é impossível do seu lado — por isso ela está
escrita aqui como compromisso, e por isso a remoção tem prazo mínimo em vez de "quando der".)*

### As chaves de COBRANÇA — a projeção de custo vira medição (2026-07-28)

**Pedido de um consumidor em 2026-07-28.** O painel de Consumo dele multiplica volume por tarifa, com
o volume **estimado do lado dele**. A Meta manda, em cada webhook de status, sob qual categoria ela
cobrou aquela entrega — o mesmo `cobranca` que já viaja no envelope. Estas sete chaves são esse dado
contado, e é a regra da casa aplicada a dinheiro: **número que o gateway promete tem de vir do
gateway.**

| chave | o que conta |
|---|---|
| `cobranca_marketing` | entregas cobradas sob `marketing` |
| `cobranca_utility` | idem, `utility` |
| `cobranca_authentication` | idem, `authentication` |
| `cobranca_service` | idem, `service` |
| `cobranca_outra` | a Meta cobrou sob uma categoria que o gateway **ainda não conhece** |
| `cobranca_ausente` | o `sent` chegou **sem** o bloco de cobrança — não é erro, ver abaixo |
| `cobranca_cobravel` | dos acima, aqueles em que a Meta disse `billable: true` |

**Os nomes são os valores LITERAIS da Meta** (`marketing`, `utility`, `authentication`, `service`),
com o prefixo `cobranca_` só para agrupar. Não traduzimos vocabulário dela em lugar nenhum — é a
mesma regra de `"TIER_250"` não virar `250`.

#### 🔴 Só o `sent` conta — e é isso que impede o número de ser até 3x maior

O **mesmo** envio aparece em `sent`, `delivered` e `read`, e os três podem trazer o bloco de cobrança
(medido: no nosso corpus, o `sent` e o `delivered` capturados são o mesmo `wamid`, com a mesma
categoria; e há `read` com bloco próprio). Contar todo status com cobrança multiplicaria a fatura
medida por até três — e um número inflado é **pior** que estimativa, porque parece medição.

`sent` é o único estado por onde toda mensagem cobrada passa: uma mensagem pode ser enviada e nunca
entregue (aparelho desligado por dias), e a Meta já cobrou.

#### `cobranca_ausente` é o tamanho do que você ainda precisa estimar

**~7,5% dos `sent` chegam sem bloco de cobrança** — 4 em 53 `sent` crus, medido por um consumidor
sobre 267 payloads reais em 2026-07-28. **Isso não é payload quebrado, é rotina.** A chave existe
para esse ponto cego ser um número em vez de um silêncio:

    cobranca_marketing + cobranca_utility + cobranca_authentication
      + cobranca_service + cobranca_outra + cobranca_ausente   =   `sent` contados

Sem ela, a medição sairia **menor** que a realidade sem nada acusando. Com ela, você sabe exatamente
sobre quanto volume ainda está estimando.

#### `cobranca_outra` — a Meta pode inventar categoria amanhã

Quando ela inventar, o evento **conta** em `cobranca_outra` e o gateway grava no log o **valor
literal** que chegou. Descartar em silêncio seria perder dinheiro sem saber; criar uma chave nova a
partir do valor recebido deixaria a **Meta** escolher as chaves da sua tela e da sua resposta.

**O que isso exige de você, e é uma linha:** trate `cobranca_outra` como volume **cobrado e ainda não
classificado** — some-o ao total ao projetar custo, e não o descarte por não saber a categoria. Se ele
subir de forma consistente, o dado que falta é o **valor literal** que a Meta mandou; o gateway o
grava no log dele, e do seu lado a leitura possível é a do próprio evento `status`, cujo campo
`cobranca.categoria` chega **cru** no envelope. Ou seja: **a categoria nova está no seu `cru` antes de
estar em qualquer contador nosso** — se você precisa dela hoje, ela está lá.

#### `cobranca_cobravel` conta só o `true`

`billable: false` e `billable` ausente são fatos diferentes, mas **nenhum dos dois é "cobrado"**, e
inventar cobrança a partir de ausência erra no sentido caro. Consequência: `cobranca_cobravel` é
sempre **menor ou igual** à soma das categorias, e a diferença é *"não cobrado"* mais *"a Meta não
disse"*. O `service` que capturamos no tráfego real, por exemplo, veio com `billable: false`.

#### O que estes números NÃO são

Eles contam **webhooks de `sent` aceitos**, não mensagens únicas. O gateway não guarda estado por
mensagem — é a mesma fronteira que faz dele um gateway e não uma fila —, então uma reentrega da Meta
depois de um `200` contaria de novo. É o mesmo caveat que `recebidas` e `entregues` sempre tiveram.

**O caso comum de reentrega está coberto:** a contagem só acontece quando a resposta à Meta foi
`2xx`. Se o seu callback estiver fora do ar e a gente responder `5xx`, a Meta reenvia — e nada é
contado até o webhook ser de fato aceito. Sem essa regra, um incidente do seu lado inflaria
justamente o número que você mais olha durante ele.

### `token_meta` — a checagem viva, com DOIS carimbos e TRÊS estados

Responde *"a Meta ainda aceita o token desta instância?"* — a mesma pergunta do
`GET /v1/instances/{slug}/health`, mas **sem custo por chamada**: aqui você lê o que o gateway já
mediu.

| campo | o que é |
|---|---|
| `veredito` | `"ok"` · `"recusado"` · `"desconhecido"` |
| `medido_em` | quando a Meta **respondeu** pela última vez (`null` = nunca) |
| `conferido_em` | a última **tentativa**, com ou sem sucesso (`null` = nunca) |
| `checagem_falhando_desde` | início da sequência atual de falhas de checagem (`null` = não está falhando) |

**Por que dois carimbos, e não um.** `{"veredito":"ok","medido_em":"15:20"}` sozinho é **ambíguo
entre dois estados opostos**: *"conferi às 15:20 e não precisei conferir de novo"* e *"conferi às
15:20, e todas as tentativas desde então falharam"*. No segundo caso o seu painel pintaria verde com
a Meta fora do ar. **`medido_em` e `conferido_em` divergindo é o sinal de que a checagem está
falhando** — visível sem você saber nada da nossa implementação.

**O `ok` velho EXPIRA.** Passados 15 minutos sem a Meta responder, o veredito degrada para
`desconhecido` em vez de continuar `ok`: cache que nunca expira é mentira com carimbo. `medido_em`
continua apontando para a última resposta real — é ele que diz há quanto tempo o gateway não ouve a
Meta.

**`desconhecido` não é vocabulário novo:** é a mesma palavra da `classe` de erro do envio, com o
mesmo significado — *não sabemos*. Disfarçar "não sabemos" de qualquer das outras duas é que causa
dano.

- `recusado` = a Meta recusou a credencial, **ou** o `phone_number_id` cadastrado é inválido e a
  chamada nem saiu do gateway. Nos dois, **só gente conserta** — a ação é a mesma, por isso a palavra
  é a mesma;
- `desconhecido` = ninguém mediu ainda (gateway recém-subido, instância pausada) ou a medição
  envelheceu.

**Quem mede é um timer nosso, rodando por instância ATIVA a cada 5 minutos**, independente de haver
tráfego e de alguém estar olhando. Isso é deliberado: se o veredito dependesse de tráfego, *ausência
de tráfego* ficaria indistinguível de *sistema quebrado* — token revogado às 2h da manhã só apareceria
às 8h, na frente da primeira cliente. **Instância pausada não é medida** (ela não envia), e por isso o
veredito dela envelhece para `desconhecido`; use `pausada` para não confundir os dois.

**As duas regras de alarme que isto lhe dá, e nenhuma exige conhecer as nossas entranhas:**
`veredito != "ok"`, ou `conferido_em` envelhecido.

### `certificado_do_callback` — a validade do **seu** certificado, como o gateway a viu

O irmão do `token_meta` do outro lado: aquele responde *"a Meta ainda aceita o token desta
instância?"*; este responde *"o certificado do seu callback ainda vai estar válido semana que
vem?"*. **Certificado do consumidor expirando derruba a entrega inteira**, e o sintoma chega como
falha de TLS de madrugada — saber com dias de antecedência transforma incidente em manutenção.
Renovação automática existe justamente para isso, mas **automação falha calada**.

| campo | o que é |
|---|---|
| `estado` | `"observado"` · `"nunca_observado"` |
| `expira_em` | o `NotAfter` do certificado, UTC/RFC3339 (`null` em `nunca_observado`) |
| `observado_em` | quando o gateway **viu** esse certificado, UTC/RFC3339 (`null` em `nunca_observado`) |

**Não é sonda: é observação.** O gateway não abre conexão nenhuma para olhar o seu certificado — ele
lê o que o handshake **da entrega** já traz, na mesma conexão que ia acontecer de qualquer jeito.
Duas consequências que você precisa saber, e a segunda é a que decide a sua regra de alarme:

- **sem entrega, não há observação nova.** O dado envelhece quando o tráfego para (ou quando a
  instância fica pausada). É por isso que `observado_em` viaja junto: certificado observado há três
  semanas **não é informação atual**, e o gateway não tem como fingir que é;
- **é o certificado FOLHA** (o seu), não a cadeia inteira. É o que renova a cada ~90 dias e o que
  quebra quando a renovação falha. Intermediária da sua CA expirando não aparece aqui.

**`nunca_observado` é um estado com nome, e isso é deliberado — trate-o como "sem informação", nunca
como "vencido".** Ele significa que **nenhuma entrega desta instância chegou a completar um
handshake**: instância recém-criada, ou consumidor que ainda não recebeu nada. Não é falha, e não é
motivo de alarme sozinho.

> **Por que a palavra, e não só `null`.** Esta rota já pagou essa conta uma vez: ela subiu com
> `ultimo_em: null` em contadores que **tinham** histórico (o carimbo só passou a ser gravado
> naquele momento), e quem tratasse `null` como "muito antigo" começaria com falso positivo em
> tudo. Aqui o caso é permanente — instância sem entrega nunca terá data —, então a diferença entre
> *"nunca vi"* e *"vi e está ruim"* é **inequívoca na forma**: uma palavra que não se parece com
> data nenhuma e não se compara com data nenhuma. O campo também **não some** da resposta: campo
> ausente obrigaria você a distinguir "ausente" de "nulo" para responder a mesma pergunta.

**O gateway não diz "expirado" nem "expira em N dias", e isso também é decisão.** Seria juízo sobre
uma observação que pode estar velha — o certificado pode ter sido renovado depois dela. Com os dois
carimbos na mão você faz a conta melhor do que nós, inclusive decidindo quanta idade de observação
tolera.

**A regra de alarme sugerida**, e ela usa os dois campos de propósito:

```
estado == "observado"
  E expira_em - agora < 14 dias
  E agora - observado_em < 24 h      # senão você está alarmando sobre informação velha
```

E, separado disso: `estado == "observado"` com `observado_em` muito velho **numa instância ativa e
com tráfego** quer dizer que a entrega parou — mas para isso `entregues.ultimo_em` responde melhor,
porque ele existe exatamente para essa pergunta.

Exemplo do bloco numa instância que ainda não entregou nada (**colado da mesma execução** do exemplo
acima, antes da observação):

```json
"certificado_do_callback": {
  "estado": "nunca_observado",
  "expira_em": null,
  "observado_em": null
}
```

### `numero_na_meta` — a **qualidade** e o **limite de mensagens** do seu número (2026-07-28)

O terceiro irmão de `token_meta` e `certificado_do_callback`. Os dois primeiros respondem *"esta
credencial continua funcionando?"*; este responde a pergunta vizinha e igualmente cara: **"este
número continua podendo enviar o volume que eu planejei?"**

**Ele existe porque você o perdeu.** Até 2026-07-28 esses dois valores eram lidos direto na Graph API
com o seu token. A regra do gateway — *ninguém fala direto com a Meta* — fechou aquele caminho, e
esta é a porta que o substitui. O que cada um decide do seu lado:

- **`limite_de_mensagens`** é o *tier* — o teto diário de conversas iniciadas. Ele **muda sozinho**:
  a conta amadurece e sobe, ou é rebaixada e cai. Planejar o mês com o tier velho é planejar errado;
- **`qualidade`** é o aviso **antecipado** de que a conta caminha para restrição. Descobrir isso pelo
  bloqueio é descobrir tarde.

```jsonc
"numero_na_meta": {
  "qualidade": {
    "estado": "observado",
    "valor": "GREEN",
    "observado_em": "2026-07-28T20:36:58Z",
    "fonte": "medicao"
  },
  "limite_de_mensagens": {
    "estado": "observado",
    "valor": "TIER_50",          // rebaixado, e o aviso chegou EMPURRADO
    "observado_em": "2026-07-28T20:40:03Z",
    "fonte": "webhook"
  },
  "conferido_em": "2026-07-28T20:36:58Z"
}
```

*(Os dois exemplos deste bloco — este e o do `nunca_observado` abaixo — são **colados de uma execução
do handler de verdade**, nunca digitados.)*

| campo | o que é |
|---|---|
| `<valor>.estado` | `"observado"` · `"nunca_observado"` — as **mesmas** palavras do `certificado_do_callback`, porque a pergunta é a mesma |
| `<valor>.valor` | o literal da Meta (`null` em `nunca_observado`) |
| `<valor>.observado_em` | quando o **gateway** soube desse valor, UTC/RFC3339 (`null` em `nunca_observado`) |
| `<valor>.fonte` | `"medicao"` · `"webhook"` (`null` em `nunca_observado`) |
| `conferido_em` | a última vez que o gateway **tentou medir**, UTC/RFC3339 (`null` se nunca tentou) |

#### 🔴 Os valores são LITERAIS da Meta — `"TIER_250"` não vira `250`

`"TIER_250"`, `"TIER_1K"`, `"GREEN"`, `"UNKNOWN"` chegam **exatamente** como a Meta os manda. O
gateway não traduz para número, não ordena as qualidades e não deriva "ruim" de nenhuma palavra.
Traduzir exigiria uma tabela nossa, e ela erraria no dia em que a Meta inventasse um valor novo —
errando do jeito pior: devolvendo um número plausível para algo que ninguém conferiu. **Se você
precisa de um número, faça a conversão do seu lado, e trate valor desconhecido como desconhecido.**

É a mesma regra do evento `qualidade_do_numero`, e por construção: os dois carregam o mesmo literal.

#### As DUAS fontes, e quem vence quando elas discordam

O `limite_de_mensagens` chega por dois caminhos, e o campo `fonte` diz qual produziu o valor que você
está lendo:

- **`medicao`** — o gateway pergunta à Graph API, por instância **ativa**, no mesmo ciclo em que já
  confere o token. Ela garante que uma instância nova tem valor mesmo que nada mude nunca;
- **`webhook`** — a Meta **empurra** `phone_number_quality_update` quando o limite muda. É o único
  caminho que chega em segundos, e é ele que avisa de um **rebaixamento antes de o envio falhar**.

**Vence a observação mais RECENTE, qualquer que seja a fonte.** Não há fonte preferida: "webhook
sempre vence" deixaria um reenvio da Meta (ela reenvia por até 36 h) regredir um valor já medido
depois; "medição sempre vence" jogaria fora justamente o aviso empurrado.

**A `qualidade` tem uma fonte só (`medicao`)**, e isso não é lacuna: o webhook
`phone_number_quality_update` **não carrega nota de qualidade** — ele carrega um `event`
(`ONBOARDING`/`FLAGGED`/`UNFLAGGED`), que é outro fato. Inventar uma equivalência entre eles seria
afirmar uma tradução que a documentação da Meta não sustenta.

#### `observado_em` é o relógio do GATEWAY, não o da Meta

O carimbo diz **quando o gateway soube**, e não quando a Meta registrou a mudança. É a mesma
definição de `certificado_do_callback.observado_em`. O motivo é que a alternativa compararia dois
relógios que ninguém sincronizou — e um desvio de minutos decidiria, em silêncio, qual fonte vence.

#### Os DOIS carimbos, e o que a divergência entre eles significa

`conferido_em` andando enquanto os `observado_em` ficam parados quer dizer **"o gateway está medindo
e voltando sem o dado"** — a Meta parou de mandar os campos, ou o pedido de campos foi recusado.
Nesse estado o valor que você lê continua verdadeiro *para a data dele*, e o `observado_em` é o que
te diz quanta idade ele tem.

**`conferido_em: null` numa instância `pausada` é o esperado**, não uma falha: instância pausada não
é medida de propósito — ela não envia, e gastar chamada por ela seria medir um canal que não pode
falhar.

#### `nunca_observado` é estado com nome — trate como "sem informação", nunca como "ruim"

O estado é **por valor**, e não do bloco, porque o caso misto existe de verdade: um webhook de limite
pode chegar antes da primeira medição, e aí o limite está observado e a qualidade não.

```json
"numero_na_meta": {
  "qualidade": {
    "estado": "nunca_observado",
    "valor": null,
    "observado_em": null,
    "fonte": null
  },
  "limite_de_mensagens": {
    "estado": "nunca_observado",
    "valor": null,
    "observado_em": null,
    "fonte": null
  },
  "conferido_em": null
}
```

**A regra de alarme sugerida** — e ela é sua, porque o gateway não publica juízo aqui:

```
limite_de_mensagens.estado == "observado"
  E limite_de_mensagens.valor != <o tier que você planejou>     # subiu ou caiu sem você saber
qualidade.estado == "observado" E qualidade.valor != "GREEN"    # e trate valor DESCONHECIDO como desconhecido
```

#### 🔴 Numa instância Instagram, este bloco (e `token_meta`) dizem `nao_se_aplica` (T-099)

**Qualidade e tier de mensagens são conceitos do WhatsApp Business Number — Instagram não os tem, e
nunca vai ter.** Até a v0.36.0 este bloco saía `nunca_observado` numa instância Instagram (medido em
produção, `tenant-two-ig`, 2026-07-30 21:11), o que é a resposta ERRADA: `nunca_observado` diz
*"ainda não medimos, espere"*; a resposta certa é `nao_se_aplica`, que diz *"nunca vai existir aqui,
não olhe"*. Se você ler `nunca_observado` num campo que nunca vai ser preenchido, ou fica esperando
para sempre ou alarma por algo que não existe — é o mesmo problema que `token_instagram` já resolve
do lado do WhatsApp (seção seguinte), e as duas direções agora usam a **mesma palavra**.

```json
"numero_na_meta": {
  "qualidade": { "estado": "nao_se_aplica", "valor": null, "observado_em": null, "fonte": null },
  "limite_de_mensagens": { "estado": "nao_se_aplica", "valor": null, "observado_em": null, "fonte": null },
  "conferido_em": null
}
```

**E o mesmo vale para `token_meta.veredito`, que também sai `"nao_se_aplica"` numa instância
Instagram.** O motivo é mais sutil do que "o campo não se aplica por definição": a checagem viva
(`watchdog.go`) mede chamando `GET /{phone_number_id}` na Graph API, e uma instância Instagram **nunca
tem** `phone_number_id` (o cadastro recusa se vier preenchido). Sem este tratamento, a vigia mediria
com o campo vazio, a Graph recusaria a chamada localmente (nem chega a haver requisição de rede), e o
gateway classificaria isso como **credencial recusada** — um `veredito: "recusado"` **permanente e
falso** em toda instância Instagram saudável, porque a checagem nunca foi desenhada para medir nada
por lá. Por isso o gateway não deixa esse resultado vazar: `token_meta` também vira `nao_se_aplica`.

### `token_instagram` — a validade do token de longa duração do Instagram (2026-07-30)

Responde *"este canal Instagram vai continuar funcionando?"* — pelo lado do **token**, que aqui tem
uma diferença dura em relação ao WhatsApp: ele **vence em 60 dias**, sempre, e passado esse prazo
**não há renovação possível** — só um login manual na Meta. O gateway tenta renovar sozinho, com
folga, mas isto é o único bloco de todo o `GET /v1/estado` em que "não fizemos nada" tem um
desfecho **definitivo**, não uma zona cinza.

🔴 **Este campo aparece em TODA instância, mesmo WhatsApp — a ausência é AFIRMADA, nunca inferida.**
`GET /v1/estado?instancia=<slug>` é **um endpoint só, uma chamada por instância** (o vínculo
consumidor→instância decide qual). Numa instância **WhatsApp** este bloco sai assim, sempre:

```json
"token_instagram": {
  "veredito": "nao_se_aplica",
  "definido_em": null,
  "expira_em": null,
  "dias_restantes": null,
  "renovado_em": null,
  "falhando_desde": null,
  "instrucao": null
}
```

**Isto NÃO é o bloco quebrado — é o bloco dizendo a verdade.** O token de System User que o
WhatsApp usa não tem prazo de 60 dias (é a mesma razão pela qual `numero_na_meta` e `token_meta`
sempre saem `nao_se_aplica` do lado do Instagram — ver a subseção correspondente, acima —, e
`token_instagram` sempre sai `nao_se_aplica` do lado do WhatsApp: cada produto da Meta tem a
credencial que tem, e o bloco existe SEMPRE, na resposta, dizendo qual dos dois é o seu caso). Um
campo que simplesmente sumisse, ou viesse com os números zerados em vez de `null`, faria você achar
que a renovação automática está quebrada quando ela nunca existiu ali.

| campo | o que é |
|---|---|
| `veredito` | `"nao_se_aplica"` · `"aguardando"` · `"ok"` · `"falhando"` · `"expirado"` |
| `definido_em` | quando o token ATUAL foi definido — criação, seu cadastro, rotação do dono, ou a última renovação automática (`null` em `nao_se_aplica`) |
| `expira_em` | `definido_em` + 60 dias (`null` em `nao_se_aplica`) |
| `dias_restantes` | pode ser **negativo** (expirado há N dias) (`null` em `nao_se_aplica`) |
| `renovado_em` | a última vez que o **laço automático** renovou este token com sucesso — `null` até a primeira renovação de verdade, mesmo que o token original ainda tenha dias de vida |
| `falhando_desde` | início da sequência atual de falhas de renovação (`null` = não está falhando) |
| `instrucao` | texto explicando o que fazer — só presente quando `veredito` é `falhando` ou `expirado` |

**Os cinco vereditos, e o que cada um pede de você:**

- **`aguardando`** — token válido, ainda longe do limiar de renovar (a partir de 30 dias de idade).
  Normal, sem ação nenhuma;
- **`ok`** — o laço automático **já renovou este token com sucesso pelo menos uma vez**. É a
  resposta a *"o mecanismo funciona de verdade?"* — e é por isso que ele é um veredito PRÓPRIO, e não
  o mesmo que `aguardando`: um token que nunca precisou renovar ainda não provou nada sobre a
  automação;
- **`falhando`** — a tentativa de renovação mais recente não deu certo (a Meta recusou, ou a
  gravação do token novo falhou) e o token **ainda não venceu**. `falhando_desde` mostra a HONESTA
  primeira falha — não há atraso nem limiar aqui: se o gateway está falhando há 10 minutos, é isso
  que a resposta diz há 10 minutos;
- **`expirado`** — passou de 60 dias sem renovar. **Não há mais renovação automática possível.**

#### 🔴 Quem alarma é você — o gateway só registra

**Decisão do dono, 2026-07-30.** O gateway **não** manda notificação, não escala e não abre chamado
quando a renovação falha — ele grava uma linha `ALARME` no próprio log (operacional, não chega até
você) e deixa o ESTADO honesto para você ler. **Você já tem canal para falar com quem opera este
gateway; construir um segundo canal aqui seria pior que o que já existe do seu lado.**

🔴 **E o motivo pelo qual isto importa mais aqui do que em qualquer outro bloco: você não consegue
consertar sozinho.** O token não está na sua mão, por desenho deste gateway (ninguém fala direto com
a Meta). Por isso `instrucao` não é cosmético — ela é a única coisa que separa `veredito: "falhando"`
de um beco sem saída para quem não tem acesso ao problema.

**Regra prática de alarme, sua:**

```
veredito == "expirado"                                    # pare tudo, é manual
  OU (veredito == "falhando" E falhando_desde tem mais de alguns dias)
```

Se `falhando_desde` passar de alguns dias, acione o dono da conta Instagram na Meta — a resolução é
**manual** e não está do lado do gateway. Um `falhando` recém-aparecido normalmente se resolve
sozinho no próximo ciclo (rede instável, um `5xx` passageiro da Meta); é a PERSISTÊNCIA da falha que
pede gente, não a primeira ocorrência.

Exemplo de uma instância Instagram falhando (colado de uma execução do handler de verdade):

```json
"token_instagram": {
  "veredito": "falhando",
  "definido_em": "2026-06-15T00:00:00Z",
  "expira_em": "2026-08-14T00:00:00Z",
  "dias_restantes": 12,
  "renovado_em": null,
  "falhando_desde": "2026-08-01T09:00:00Z",
  "instrucao": "a renovacao automatica esta falhando; a resolucao e MANUAL, do lado de quem opera o gateway ou e dono da conta Instagram na Meta — o token nao esta ao alcance deste consumidor"
}
```

### `entrada` — por ONDE a entrada é publicada, e se o conector está de pé (2026-08-06)

🔴 **LEIA ESTA FRASE ANTES DE USAR O BLOCO, porque ela é o que impede o mal-entendido caro:**
`via` e `conector` descrevem **por onde a entrada é publicada** e **se o conector está de pé** —
eles **NÃO** prometem que a Meta está conseguindo entregar. **Quem responde essa pergunta é uma
sonda, medindo de FORA.**

**Por que o gateway não pode responder isso, e não é falta de vontade:** requisição que não chega
não deixa rastro nenhum aqui dentro. Se o caminho público cair, o journal não registra o que não
chegou, a inscrição na Meta continua correta, os contadores só param de subir e o `/v1/health`
continua `200`. Em **2026-08-06** o link caiu por ~9 minutos com todos os monitores verdes, e quem
avisou foi o consumidor. **Um campo `alcancavel: true` seria exatamente esse monitor cego** — por
isso ele não existe aqui, e não vai passar a existir.

**O que você pode fazer com o bloco, então:**

| campo | é | serve para |
|---|---|---|
| `via` | **configuração**, não medição — `tunel`, `encaminhamento_de_porta` ou `desconhecido` | saber por onde a entrada deveria estar chegando quando você for reportar uma queda |
| `conector` | **medição** do `/ready` do conector que publica a rota | distinguir "o túnel caiu" de "o gateway está quieto" |
| `ultimo_webhook_em` | o **mesmo** valor de `contadores.recebidas.ultimo_em` | concluir **silêncio** por conta própria, sem ler a tabela de contadores |

**`conector.estado` tem TRÊS valores, e a diferença entre dois deles é o ponto do bloco:**

- **`observado`** — o gateway perguntou e o conector respondeu. `conexoes_prontas` traz o número, e
  ele **pode ser `0`**: zero é uma medição legítima ("o conector está de pé e não há túnel montado"),
  o sinal mais forte que este bloco consegue dar. `falhando_desde` vem `null`;
- **`desconhecido`** — **não consegui medir**. `conexoes_prontas` vem **sempre `null`**, nunca um
  zero que pareça veredito; `falhando_desde` diz desde quando a pergunta não volta (`null` se nunca
  houve tentativa), e `medido_em` continua apontando para a **última resposta real**, que é o que diz
  há quanto tempo o gateway não ouve o conector;
- **`nao_configurado`** — ninguém disse ao gateway a quem perguntar (instalação sem túnel). Os três
  campos vêm `null`.

⚠️ **`observado` NÃO é um veredito de saúde.** O gateway publica o que mediu e quando mediu; quem
julga é você. É a mesma regra de `certificado_do_callback`, que também não tem estado "vencido".

⚠️ **O bloco vem SEMPRE, com todas as chaves, em toda instância** — inclusive `nao_configurado` e
inclusive numa instância Instagram. Campo que some quebra parser estrito, e este contrato já pagou
por isso com o `token_instagram`.

ℹ️ **`via` e `conector` são do GATEWAY, não da instância:** duas instâncias do mesmo gateway leem
exatamente os mesmos valores. Só `ultimo_webhook_em` é por instância.

**A regra de alarme que isso te dá:** `conector.estado == "observado" && conexoes_prontas == 0` é
*"o túnel caiu"* — aja. `conector.estado == "desconhecido"` é *"o gateway não está conseguindo
medir"* — outra urgência, outro lugar para procurar, e **nunca** o mesmo alarme.

### `alcance_externo` — o veredito da sonda pública, espelhado aqui (2026-08-07)

🔴 **LEIA A SEÇÃO "Duas perguntas diferentes" (acima) ANTES DE USAR ESTE BLOCO.** Ele é
**conveniência**, não uma segunda fonte: quando o gateway está calado, este bloco também está — é a
URL pública da sonda (seção acima) que sobrevive à nossa queda, nunca este campo.

```jsonc
"alcance_externo": { "estado": "observado", "veredito": "up", "medido_em": "2026-08-07T13:05:00Z",
                      "fonte": "sonda_externa" }
```

| campo | é |
|---|---|
| `estado` | `observado`, `nao_configurado` ou `nao_consegui_verificar` — ver abaixo |
| `veredito` | o literal que a sonda externa respondeu (hoje, `"up"` ou `"down"`), **sem tradução** — `null` fora de `observado` |
| `medido_em` | a última vez que a sonda externa RESPONDEU de verdade — continua apontando pra essa resposta mesmo depois de o estado degradar, pela mesma razão de `token_meta.medido_em` |
| `fonte` | hoje sempre `"sonda_externa"` quando `observado`; existe para o dia em que um segundo mecanismo entrar, sem forçar você a reinterpretar o contrato |

**`estado` tem TRÊS valores, e a distinção entre os dois últimos é o ponto do bloco:**

- **`observado`** — o gateway perguntou à sonda externa e ela respondeu. `veredito` traz o literal
  dela, **incluindo `"down"`** — down MEDIDO é `observado` com `veredito: "down"`, não um estado à
  parte;
- 🔴 **`nao_consegui_verificar`** — a ÚLTIMA tentativa do gateway de perguntar à sonda externa não
  voltou (sem resposta, sem JSON legível, ou sem o campo esperado), OU a última resposta boa já
  passou da validade. **Isto NUNCA é `down`, e nunca é o campo ausente.** É uma palavra própria,
  diferente de `desconhecido` usado em `token_meta`/`conector` — porque a decisão que você vai
  automatizar em cima dela é diferente: "eu não consegui perguntar" não é "vocês estão fora do ar";
- **`nao_configurado`** — este gateway ainda não tem `ZAPGW_SONDA_EXTERNA_URL` configurada. `veredito`,
  `medido_em` e `fonte` vêm `null`.

⚠️ **O bloco vem SEMPRE, com as quatro chaves, em toda instância** — mesma regra de `entrada` e
`token_instagram`: campo que some quebra parser estrito.

**A regra de alarme que isso te dá:** `alcance_externo.estado == "observado" && veredito == "down"`
é *"a entrada pública caiu"* — aja, com a MESMA urgência de uma leitura direta da sonda.
`alcance_externo.estado == "nao_consegui_verificar"` é *"o gateway não conseguiu perguntar à sonda
agora"* — não é sinal de queda nenhuma; se você quiser saber mesmo assim, use a URL direta desta
seção, que não depende do gateway responder.

### Erros

| status | quando | corpo |
|---|---|---|
| `400` | falta o parâmetro `instancia` | `{"erro":{"classe":"permanente","mensagem":"parametro de consulta \`instancia\` e obrigatorio"}}` |
| `401` | sem token, ou token que ninguém reconhece | o corpo de erro padrão |
| `403` | a instância não é sua | `{"erro":{"classe":"config","mensagem":"instancia nao autorizada para este consumidor"}}` |
| `404` | slug que não existe | `{"erro":{"classe":"config","mensagem":"instancia desconhecida"}}` |
| `503` | o gateway não conseguiu ler o próprio banco | `{"erro":{"classe":"retentavel","mensagem":"indisponivel"}}` |

Os corpos de `400` e `403` acima são **colados de uma execução**, não digitados.

> **Falta o parâmetro é `400`, não `403`.** Um `403` ali mandaria você conferir o seu vínculo, que
> está certo — o defeito ficaria escondido no lugar errado.

### O que esta rota deliberadamente NÃO é

- **Não é Prometheus.** O público é você, não um coletor. Formato de scraping convidaria infra que
  este projeto não tem e que a premissa "rodar fora do homelab um dia" torna incerta;
- **não aceita filtro de data arbitrário**, nem histórico por evento. As janelas são as duas que o
  gateway já mostra a quem opera. Superfície que cresce sem consumidor pedindo é superfície morta que
  lê como atual;
- **não conta por categoria de cobrança.** Isso é contador novo no caminho de entrada, não formato de
  resposta — está registrado como tarefa própria.

---

## Ler o catálogo de templates

**`GET /v1/templates?instancia={slug}`** · `Authorization: Bearer <seu token>` ·
opcional: `&status=APPROVED`

Mesmas regras do `/v1/messages`: rota da **LAN**, no entrypoint interno (`:8443`), e só as instâncias
vinculadas a você respondem — `403` para as outras, sem que o gateway chegue a falar com a Meta. O
catálogo descreve o negócio do inquilino (nomes de campanha, texto de cobrança), então a separação
vale aqui pelo mesmo motivo do envio.

```jsonc
{ "instancia": "lojinha",
  "total": 84,
  "templates": [
    { "id": "1234567890",
      "nome": "lembrete_consulta",
      "status": "APPROVED",
      "categoria": "UTILITY",
      "idioma": "pt_BR",
      "componentes": [ /* exatamente como a Meta os devolve, sem reescrita */ ] },
    { "id": "1234567891",
      "nome": "acessar_galeria",
      "status": "REJECTED",
      "categoria": "UTILITY",
      "idioma": "pt_BR",
      "motivo": "INCORRECT_CATEGORY",
      "componentes": [ /* … */ ] }
  ] }
```

> **`id` é o id do template na Meta — o mesmo que o `POST /v1/templates` devolve.** Ele entrou em
> 2026-07-28 e **pode faltar**: o gateway não exige o campo, porque não foi verificado na fonte da Meta
> que toda página do catálogo o carrega, e derrubar a leitura inteira do catálogo por causa dele
> seria trocar um catálogo útil por nenhum. Quando falta, a chave simplesmente não vem. **Continue
> identificando template pelo par `nome` + `idioma`** — é ele que vai no envio, e é ele que a Meta
> trata como único dentro da conta.

> **`motivo` é o `rejected_reason` da Meta, cru — entrou em 2026-08-04 (T-116) para responder "por
> que foi recusado?" sem exigir leitura manual do WhatsApp Manager.** Custo real que motivou a tarefa:
> o `consumer-b` recusou `acessar_galeria`, formulou hipótese às cegas, criou `acessar_galeria_v2`,
> recusado de novo — duas tentativas, zero informação nova, e cada tentativa **queima um nome de
> template para sempre** (a Meta não libera o nome de um template rejeitado para reuso imediato).
>
> - **`"NONE"` é valor normal, não ausência** — mesma doutrina do `template.motivo` do webhook
>   `template_status` (seção acima, ~linha 974): a Meta manda a string literal `"NONE"` quando não há
>   motivo, e o gateway não traduz para vazio. Se a chave `motivo` sumir do JSON, é porque a Meta
>   realmente não mandou `rejected_reason` naquele item — "disse NONE" e "não mandou o campo" são
>   fatos diferentes.
> - **O texto em prosa do WhatsApp Manager NÃO vem, e não vai vir enquanto a Meta não expuser campo
>   para ele.** `rejected_reason` é um enum curto (`ABUSIVE_CONTENT`, `INVALID_FORMAT`, `NONE`,
>   `PROMOTIONAL`, `SCAM`, `TAG_CONTENT_MISMATCH`, `INCORRECT_CATEGORY` observado nos webhooks) — o
>   parágrafo explicativo que aparece no painel não tem campo documentado na Graph API, e o gateway
>   não sintetiza texto a partir do enum: seria doc falsa em forma de campo.
> - **O mesmo motivo já chega pelo webhook `template_status`** (`template.motivo`, seção acima,
>   ~linha 974) **no instante em que a Meta decide** — sem você precisar perguntar. Este campo aqui é
>   para quando você lê o catálogo por outro motivo (ou perdeu o webhook); o webhook é o caminho que
>   avisa sozinho.

**A lista vem INTEIRA, ou vem erro — nunca uma lista curta.** Esta é a razão de o endpoint existir: o
gateway antigo desta rede devolvia só os **25 primeiros** templates de uma conta com **84**, e foi o
que o tirou de produção. O truncamento não dava erro nenhum, então de fora ele era indistinguível da
verdade: o sistema consumidor concluía que o template "não existe" e a mensagem simplesmente nunca
saía. Aqui o gateway segue a paginação da Meta **até ela acabar**, e se algum dia o catálogo não
couber no limite de paginação dele (**50 páginas de 100**, ~5000 templates), você recebe **`502` com
`classe: config`** e **nenhuma lista** — de propósito.

**Se isso acontecer, repetir não resolve — e a única saída que está na sua mão é `&status=`.** O
parâmetro é repassado à Meta na query (além de ser reaplicado aqui), então, **se ela o honrar**, a
leitura percorre menos páginas e volta a caber. É paliativo, e está escrito como paliativo: se a Meta
ignorar o filtro, o `502` continua. Vale tentar antes de qualquer outra coisa, porque custa uma query
string, e aprovados são de todo modo o subconjunto que interessa para enviar.

`status` filtra (`APPROVED`, `PENDING`, `REJECTED`, …). O filtro é aplicado **também do lado do
gateway**, então `status=APPROVED` significa aprovado mesmo — sem depender de a Meta honrar o
parâmetro.

**Não há cache**, e isso é deliberado: um template muda de status na Meta **sem avisar o gateway**,
e um catálogo guardado responderia `APPROVED` sobre algo que acabou de ser rejeitado. Em troca, cada
chamada custa uma ida à Graph API na conta daquela instância — leia o catálogo quando precisar dele,
não num laço apertado.

`componentes` viaja **cru, como a Meta o devolve**. O gateway não reescreve nem valida essa parte:
descrever aquele formato seria congelar uma forma que é dela, não nossa.

## Criar um template

**`POST /v1/templates`** · `Authorization: Bearer <seu token>`

Mesmas regras do `GET` irmão e do `/v1/messages`: rota da **LAN**, no entrypoint interno (`:8443`), e
só as instâncias vinculadas a você respondem — `403` para as outras, sem que o gateway chegue a falar
com a Meta. **É o mesmo caminho e a mesma porta do `GET /v1/templates`**; a única diferença é o verbo.

```jsonc
{ "instancia": "lojinha",
  "nome": "lembrete_consulta",
  "categoria": "UTILITY",
  "idioma": "pt_BR",
  "componentes": [ { "type": "BODY", "text": "Olá {{1}}, sua consulta é amanhã." } ] }
```

`allow_category_change` é **opcional** e, se você mandar, viaja **verbatim** para o `allow_category_change`
da Meta — o gateway não valida, não interpreta, não traduz o valor; quem decide o efeito é ela. Se você
não mandar o campo, o gateway **não manda nada** a respeito para a Meta (não existe um `false` implícito
saindo em silêncio no seu lugar).

> ⚠️ **O que se sabe sobre este parâmetro, e o que NÃO se sabe — leia antes de contar com ele.** A Meta
> documenta (páginas lidas em 2026-07-30:
> [`new-template-guidelines`](https://developers.facebook.com/docs/whatsapp/updates-to-pricing/new-template-guidelines/)
> e
> [`template-categorization`](https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-categorization))
> que, desde **2025-04-09**, a recategorização automática **virou o comportamento padrão**: pedir
> `UTILITY` num conteúdo que ela classifica como `MARKETING` resulta em *"the template is approved as
> `MARKETING`"* — é o comportamento que `allow_category_change: true` ligava antes dessa data.
>
> **O que NÃO está confirmado, e o gateway não promete:** se mandar `allow_category_change: false`
> ainda faz a Meta **recusar** a criação em vez de recategorizar. A documentação não afirma isso — ela
> só descreve o que `true` fazia antes de virar padrão. **O gateway apenas repassa o campo**; ele não
> garante efeito nenhum sobre o desfecho, e não há como garantir algo que a fonte não documenta.
>
> **A recategorização não se desfaz por esta API, com `allow_category_change` ou sem.** O caminho que
> existe é o *category review request*, disponível **só pelo WhatsApp Manager** (Business Support Home
> → *Template Category Updates*), para template `MARKETING` com status `APPROVED`, ou `UTILITY`/`MARKETING`
> com status `REJECTED`, dentro de **60 dias** da mudança de categoria. É ação humana do dono da conta —
> **o gateway não tem rota para isso e não vai ter, porque não existe endpoint** para essa revisão na
> Graph API.

> ⚠️ **Aqui `componentes` é o formato CRU da Meta — e isso é o CONTRÁRIO do que vale no envio.**
> No `POST /v1/messages` o `components` cru é **recusado** (`ErrRawComponents`): lá o gateway modela
> `cabecalho` e `botoes_template` e monta o bloco, para tornar inexprimível o que a Meta rejeita. Aqui
> ele repassa a lista como veio, e a única guarda é que seja uma **lista JSON de verdade** (`null` e
> `{}` não falham o `Unmarshal` e viajariam como template sem corpo).
>
> **A assimetria é deliberada, e o critério é o modo de falha:**
>
> | | Se você errar o formato |
> |---|---|
> | **envio** | a Meta aceita e a mensagem chega **errada no celular do cliente** — falha calada |
> | **criação** | a Meta **recusa na hora**, com a mensagem dela, e nada foi enviado a ninguém |
>
> Onde errar é barato e o vocabulário da Meta é grande e muda, modelar custaria mais do que
> protegeria. Onde errar é caro e silencioso, o gateway modela.
>
> **A consequência para você: quem CRIA template precisa conhecer o formato da Meta; quem ENVIA,
> não.** Use a referência de componentes de template da Meta para montar esta lista.

Sucesso → **`201`**:

```jsonc
{ "id": "1234567890",
  "status": "PENDING",
  "categoria": "UTILITY",
  "categoria_pedida": "UTILITY",
  "aviso": "template recem-criado NAO pode ser usado na hora: ele nasce PENDING e so vale depois de aprovado pela Meta. …" }
```

**Template criado NÃO pode ser usado na hora.** Ele nasce `PENDING` e só passa a valer quando a Meta
o aprova — por isso o `aviso` vem em toda criação bem-sucedida. Quem tenta enviar logo depois de
criar leva erro da Meta e não consegue explicá-lo, porque a criação respondeu sucesso. Confira o
status no `GET /v1/templates` antes de enviar.

`categoria_pedida` é o **eco do que você pediu**, sempre presente (é campo obrigatório do pedido).
`categoria` é o que **a Meta gravou** — e os dois podem ser diferentes.

> 🔴 **A Meta pode GRAVAR uma categoria diferente da que você PEDIU, sem erro e sem aviso próprio
> dela.** Relato de campo do `consumer-b` (2026-07-30): submeteram `instagram_continuar` como
> `UTILITY` e a Meta gravou `MARKETING` — só descobriram relendo o catálogo depois. Categoria decide
> **cobrança** (`MARKETING` e `UTILITY` têm preços diferentes) e se a mensagem precisa de opt-in — uma
> troca silenciosa é dinheiro e conformidade, não estética.
>
> Quando `categoria` (o que a Meta gravou) é **diferente** de `categoria_pedida` (o que você mandou,
> comparação que ignora caixa e espaço), o `aviso` ganha um segundo trecho contando as duas categorias
> com todas as letras: que a troca **NÃO é erro** — a Meta pode recategorizar um template na própria
> criação — e que o gateway **NÃO desfaz** a troca. Isso vale nos dois caminhos de sucesso: a criação
> normal e a reconstruída pela releitura do catálogo (próxima seção).

O corpo de erro é o mesmo do envio, com as mesmas classes.

### Quando a criação termina **sem resposta da Meta**

Existe um desfecho que não é sucesso nem recusa: o pedido saiu daqui e **nenhum veredito voltou**
(transporte caído, prazo estourado, ou um `2xx` sem `id`). Nesse caso o gateway **não devolve a você
uma pergunta** — ele **relê o catálogo** (`GET`, e só `GET`) e responde com o que achou. São três
respostas possíveis, e cada uma quer uma reação diferente da sua:

| O que o gateway achou na releitura | O que você recebe | O que fazer |
|---|---|---|
| **achou o template** | **`201`**, igual ao sucesso normal, com `id`, `status` e `categoria` vindos do catálogo. O `aviso` diz que a criação terminou sem resposta e que a releitura **confirmou** que o template existe | nada. **Ele foi criado.** Só a resposta se perdeu |
| **não achou** | **`502`**, classe `desconhecido`, e a mensagem contém a palavra **INCONCLUSIVO** | **não recrie às cegas.** Consulte `GET /v1/templates` daqui a alguns minutos e decida com o resultado |
| **a releitura também falhou** | **`502`**, classe `desconhecido`, dizendo que a releitura também não funcionou | espere e consulte `GET /v1/templates` |

> 🔴 **"Não achei" NÃO significa "não foi criado", e o gateway nunca vai dizer que significa.**
> A Meta documenta *read-after-write* para a **resposta do próprio `POST`** dessa edge — que é
> justamente o que não chegou —, e **não documenta nada** sobre um `GET` posterior já conter o
> template recém-criado (conferido na fonte em 2026-07-28; ela também não documenta o contrário).
> Sem essa garantia, um catálogo que não mostra o template é dúvida, não veredito.
>
> **O erro é assimétrico, e é por isso que a palavra é essa.** Se dissermos "não sei" e o template
> não existir, você perde uma conferência. Se dissermos "não foi criado" e ele existir, você recria
> — e `nome` + `idioma` são **únicos por conta**: a segunda criação volta `code 100` /
> `subcode 2388024`, e **o nome que você escolheu fica inutilizável**. Um erro custa um minuto; o
> outro custa um nome.

**A releitura é um `GET` e nada mais.** O gateway **nunca** tenta criar de novo por conta própria,
em nenhum dos três desfechos — quem decide repetir é você, com o fato na mão.

> **A releitura tenta mais de uma vez, espaçada, antes de declarar "não achou" (T-101).** O caso
> MAIS PROVÁVEL deste caminho é o template ter sido criado e o catálogo da Meta
> demorar alguns segundos para propagar — e uma única releitura imediata não dava tempo a isso
> acontecer, saindo com a MESMA CARA do desfecho raro ("não foi criado mesmo"). Agora, se a
> primeira releitura (imediata, sem espera) não achar, o gateway tenta de novo depois de pausas de
> **2 s, 5 s e 10 s** (até 4 tentativas no total) antes de desistir. Achou em qualquer uma delas →
> o desfecho é o de sucesso da tabela acima, sem exceção. Não achou em nenhuma → o `502`
> **INCONCLUSIVO** sai com o **mesmo texto de sempre** — o aviso não fica mais fraco, só mais raro.
>
> O corpo da resposta (sucesso ou erro) traz `releituras` (quantas tentativas aconteceram) e
> `espera_segundos` (quanto tempo o gateway ficou pausado entre elas), para você calibrar o timeout
> do seu lado com fato em vez de estimativa.
>
> 🔴 **O teto de espera PURA deste caminho é 17 s (2+5+10), e ele conta contra o seu prazo
> "confortável" de 30 s** (ver acima). Some a esse teto o tempo de rede da tentativa de criação
> original e de cada releitura — nenhum deles é instantâneo. Se o seu cliente usa um timeout
> apertado, ele pode desistir ANTES do gateway terminar de esperar, e você volta ao mesmo limbo, só
> que sem a nossa mensagem.

**O custo que você deve conhecer:** um pedido que cai nesse caminho pode demorar até **dois** prazos
de instância (o da criação que morreu, mais o da releitura) **mais até 17 s** das pausas espaçadas
entre releituras (zero se a primeira já achar — o caminho comum não fica mais lento). É o preço de
receber um fato em vez de uma pergunta.

> **Por que isso mudou:** até 2026-07-28 a resposta era `502` com *"o template PODE ter sido criado —
> confira o catálogo"*. Em 2026-07-28 um consumidor bateu exatamente nela criando um template que
> **tinha sido criado**, e só descobriu porque ainda tinha acesso direto à Graph API para conferir.
> Esse acesso deixou de existir — **ninguém fala direto com a Meta** —, então mandar você conferir
> deixou de ser uma resposta. Quem confere agora é o gateway.
>
> **E por que a releitura passou a insistir:** em 2026-07-30 o `consumer-b` levou o `502`
> INCONCLUSIVO ao criar o `selecao_provas_novas` — e menos de um minuto depois `GET /v1/templates`
> já trazia o template. A criação tinha funcionado; só a releitura foi cedo demais para a
> propagação do catálogo. O texto do aviso continuava certo (impediu uma recriação que queimaria o
> nome), mas o desfecho comum (criou e propagou em segundos) saía com a mesma cara do desfecho raro
> — e é isso que as pausas espaçadas resolvem.

---

## Apagar um template — `DELETE /v1/templates` (2026-08-28)

Apaga **UM** template, **por nome**, do WABA da instância. É a única rota deste gateway que
**destrói** alguma coisa do lado da Meta, e o desenho inteiro dela sai daí.

```
DELETE /v1/templates?instancia=<slug>&nome=<nome>
Authorization: Bearer <sua chave>
```

Os dois parâmetros são obrigatórios. Não há corpo.

> 🔴 **A Meta apaga em TODOS OS IDIOMAS daquele nome, de uma vez.** Verbatim da referência da edge
> `message_templates`: *"Name of template to be deleted. Deletes templates matching the name in all
> languages"*. Se o mesmo nome existe em `pt_BR` e em outro idioma, **uma chamada leva os dois** — por
> isso a resposta devolve a lista do que foi, e não um "ok".

> 🔴 **Não existe curinga, não existe lote, e isso não é política — é construção.** O `nome` tem de
> casar `^[a-z0-9_]{1,512}$`, conferido **antes** de qualquer chamada à Meta: `*`, `%`, espaço, ponto
> e maiúscula são recusados com `400` sem sair daqui. A Meta oferece exclusão em lote (`hsm_ids`) e
> este gateway **não a usa em lugar nenhum**. Um nome por chamada, sempre. Exclusão não tem desfazer,
> e um curinga que a Meta aceitasse apagaria uma família inteira.

### Os três desfechos, e por que eles não colapsam num `200 {}`

Uma limpeza de dezenas de templates **vai** ser interrompida e retomada. Se "apaguei agora" e "já não
estava lá" voltarem iguais, você não consegue relatar o que fez — e o desfecho que estraga o relatório
não é o erro, é o "não sei". Por isso são três, e o campo `desfecho` os separa:

| `desfecho` | HTTP | o que aconteceu |
|---|---|---|
| `apagado` | `200` | o template existia e a Meta aceitou a exclusão |
| `ja_nao_existia` | `200` | o nome **não estava** no catálogo. **Nada foi pedido à Meta** — é o que torna a retomada idempotente de verdade |
| *(inconclusivo)* | `502`, classe `desconhecido` | a chamada saiu e **nenhum veredito voltou**. Ver abaixo |

**Sucesso** (os dois primeiros compartilham o mesmo corpo):

```json
{
  "instancia": "seu-slug",
  "nome": "galeria_atualizada_v6",
  "desfecho": "apagado",
  "entradas": [
    {"id": "1563912508540305", "idioma": "pt_BR", "categoria": "UTILITY", "status": "APPROVED"}
  ],
  "aviso": "a exclusao apaga o template em TODOS os idiomas, e a Meta NAO aceita criar um template com o MESMO nome por 30 dias. ..."
}
```

`entradas` é o que **foi** apagado, um item por idioma, lido do catálogo antes da exclusão. Em
`ja_nao_existia` ele é `[]` — **nunca `null`**, para você não ter de tratar dois vazios diferentes.

### Os 30 dias são da Meta, e o aviso só viaja em `apagado`

> *"If you delete an approved template, you cannot create a new template with the same name for 30
> days."*
> — developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-management
> (lido em 2026-08-28)

**Trate o nome como queimado.** Se precisar recriar antes disso, escolha outro nome — a criação vai
falhar, e o erro vem da Meta, não daqui.

O aviso **não** acompanha `ja_nao_existia`: ali o gateway não apagou nada e não sabe se aquele nome
um dia existiu. Dizer que um nome que você nunca usou está queimado seria o gateway inventar uma
restrição — e o valor inteiro deste aviso é ser fato com fonte.

### ⚠️ "Ainda no catálogo" NÃO é "não foi apagado"

A mesma página da Meta:

> *"If you delete a template that has been sent in a template message but has yet to be delivered
> (for example, because the WhatsApp user's phone is turned off), the template's status is set to
> `PENDING_DELETION` and WhatsApp attempts delivery for 30 days."*

Ou seja: você vai apagar, abrir o `GET /v1/templates` e **ver o template lá**. A leitura natural
("não funcionou") está errada, e agir sobre ela custa trabalho repetido ou um chamado sobre um
defeito que não existe.

Por isso o gateway trata `PENDING_DELETION` como **exclusão aceita**, e a resposta diz isso em voz
alta, num segundo aviso colado ao primeiro: *"este template CONTINUA aparecendo no catalogo com
status PENDING_DELETION … A exclusao FOI aceita; ele sai do catalogo sozinho."*

> 📊 **Documentado pela Meta, e ainda NÃO observado em tráfego: em 29 exclusões reais,
> `PENDING_DELETION` não apareceu nenhuma vez.** Primeira limpeza feita por esta rota (consumidor
> `consumer-b`, 2026-08-28, `v0.60.0`): 29 nomes, 29 desfechos `apagado`, e os 29 já tinham **sumido
> da listagem** quando o catálogo foi relido, 1 a 3 minutos depois. O fechamento da conta é o que
> torna a medição confiável: o sync do consumidor copia o `status` **verbatim, sem lista de valores
> conhecidos**, e o conjunto de status depois da limpeza saiu `APPROVED: 108`,
> `REMOVIDO_DA_META: 37`, **outros: vazio** — um `PENDING_DELETION` estaria em "outros". E
> 137 − 29 = 108.
>
> **A ressalva vale mais que o número, e é de quem mediu:** não se sabe se o `PENDING_DELETION`
> depende de haver mensagem em voo com aquele template. **Nenhum dos 29 tinha envio recente, e vários
> tinham zero mensagens na vida** — é plausível que seja exatamente esse o gatilho, e é o tipo de
> explicação confiante que ninguém aqui vai dar como fato.
>
> ➡️ **Portanto: continue tratando o caso.** O comportamento é o documentado pela Meta, e o seu código
> não pode assumir que o template sempre some na hora. O que este parágrafo acrescenta é a expectativa
> realista — **o caminho comum é sumir da listagem em poucos minutos** —, para você não desenhar a sua
> limpeza como se `PENDING_DELETION` fosse a regra.

### O `502` inconclusivo, e por que ele não diz "não foi apagado"

Se a chamada terminar **sem resposta** da Meta, o gateway relê o catálogo — imediatamente e depois em
pausas espaçadas — e reconstrói o desfecho: nome ausente **ou** todas as linhas restantes em
`PENDING_DELETION` → `apagado`, com `releituras` e `espera_segundos` no corpo dizendo o que foi
preciso para saber. **Só se o template continuar lá, vivo, sob outro status** é que sai o `502`
inconclusivo — com a palavra *inconclusivo*, nunca "não foi apagado".

A assimetria é o ponto: *"eu não vi acontecer"* não é *"não aconteceu"*, e declarar o segundo faz
você repetir uma chamada que já pode ter funcionado. É a mesma doutrina, a mesma palavra e o mesmo
laço de releitura da criação ambígua, logo acima.

### Como rodar uma limpeza em lote — de quem já rodou uma

Recomendação do consumidor `consumer-b`, que apagou 29 templates por esta rota em 2026-08-28, no
primeiro uso real dela. Nenhuma destas é exigência do gateway; todas custam minutos e valem o preço
numa operação sem desfazer.

- **Apague UM primeiro, confira a resposta viva, e só então o resto.** Até a primeira chamada real, o
  seu código só foi exercido contra o **dublê** que você mesmo escreveu — e dublê não prova nome de
  campo. Foi assim que ele confirmou que `desfecho`, `entradas[].idioma` e `aviso` chegam com os
  nomes que o comando dele lê. *Escolha para essa primeira um template inofensivo: o que ele usou
  tinha zero mensagens na vida e estava seis versões atrás.*
- **A lista sai de arquivo, e o `desfecho` é quem confere a sua edição.** A segunda leva dele começou
  de uma lista **editada à mão** (tirando o nome já apagado). Se a edição tivesse saído errada, quem
  diria era um `ja_nao_existia` — não um `200` silencioso. É para isso que os dois sucessos têm nomes
  diferentes.
- **Pare no primeiro erro, e não trate `inconclusivo` como erro nem como sucesso.** Ele é o único
  desfecho que pede olho humano: relate o nome e siga com a lista **sem** repetir a chamada.
- **Confira o resultado pelo SEU lado, não pela saída do comando.** Ele releu o catálogo depois e
  conferiu que nenhuma família tinha perdido a versão no ar — a saída do próprio comando não é prova
  disso.

### O que este gateway deliberadamente NÃO faz aqui

- **Não há teto diário de exclusões.** Um teto alto o bastante para deixar passar uma limpeza
  legítima de dezenas não segura uma corrida desgovernada que comeria o catálogo inteiro: ele estaria
  calibrado pelo conforto de quem opera, não pelo modo de falha. O freio que funciona é do seu lado
  (lista conferida, `--dry-run`, confirmação, parada no primeiro erro), porque é lá que se sabe o que
  está em uso.
- **Não há campo de confirmação booleano** (`eu_sei_que_isso_e_irreversivel` e parentes). Booleano de
  freio vira `true` no chamador uma vez e nunca mais é pensado — freio que só freia na estreia não é
  freio.
- **Não há validação de status antes de chamar.** A Meta documenta que *"Templates that are in a
  disabled status cannot be deleted"*; se for o caso, o erro **dela** sobe traduzido, com subcódigo,
  explicação e `fbtrace_id`. Adivinhar a regra da Meta aqui dentro seria trocar o motivo real por um
  palpite nosso.
- **Toda exclusão é contada** (`templates_apagados`, visível em `GET /v1/estado`) e logada, uma linha
  por chamada. Uma rajada inesperada vira número, não silêncio.

## Mandar e baixar mídia

Duas rotas, as duas da **LAN** (mesmo entrypoint `:8443` do envio) e as duas restritas às instâncias
vinculadas a você.

### Subir bytes: `POST /v1/media?instancia={slug}`

`multipart/form-data` com **uma parte chamada `arquivo`**, e o **`Content-Type` da parte** declarando
o mime. Sucesso devolve `200` e `{"media_id": "…"}` — é esse id que vai no `POST /v1/messages` com
`"tipo": "midia"`.

```
curl -H "Authorization: Bearer <seu token>" \
     -F 'arquivo=@nota.ogg;type=audio/ogg; codecs=opus' \
     'https://zapgw.exemplo.com.br:8443/v1/media?instancia=lojinha'
```

**Por que esta rota existe:** sem ela, quem tem bytes precisa hospedar uma URL pública só para a Meta
buscar — e quando ela não busca, o envio falha **calado**. Foi o que um sistema desta rede teve de
construir antes.

O gateway **não guarda os bytes**: eles atravessam em streaming para a Meta e o que sobra é o
`media_id`, que é dela. Nada de mídia vai para disco nem para log aqui.

**A lista de mimes aceitos e os tetos, para você não descobrir por `415`/`413`.** Os dois são
**do gateway, não da Meta** — ela aceita mais do que isto; nós somos conservadores de propósito,
porque mandar 40 MB para descobrir que não cabia custa o upload inteiro. A **categoria** que você
declara no `POST /v1/messages` é a mesma que decide o teto aqui, e o mime da parte é o que decide a
categoria:

| Categoria | Mimes aceitos | Teto |
|---|---|---|
| imagem | `image/jpeg`, `image/png` | 5 MiB |
| video | `video/mp4`, `video/3gpp` | 16 MiB |
| audio (inclusive nota de voz) | `audio/aac`, `audio/amr`, `audio/mpeg`, `audio/mp4`, `audio/ogg` | 16 MiB |
| documento | `application/pdf`, `application/msword`, `application/vnd.openxmlformats-officedocument.wordprocessingml.document`, `application/vnd.ms-excel`, `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`, `application/vnd.ms-powerpoint`, `application/vnd.openxmlformats-officedocument.presentationml.presentation`, `text/plain`, `text/csv` | 32 MiB |
| sticker | `image/webp` | 500 KiB |

> ℹ️ **O mime é lido pela sua parte BASE, e os parâmetros sobrevivem.** `audio/ogg; codecs=opus` casa
> a linha `audio/ogg` da tabela — e o `; codecs=opus` **não é normalizado nem descartado**, porque é
> exatamente ele que faz o WhatsApp renderizar nota de voz (veja a obrigação 5).
>
> **`image/webp` mora em `sticker`, não em `imagem`** — no WhatsApp, webp é figurinha.
>
> **Se o mime que você precisa não está na tabela, o caminho é converter o arquivo** para um dos
> aceitos antes de subir. Não há parâmetro, cabeçalho nem query que afrouxe a lista.

Recusas que acontecem **antes** de qualquer chamada à Meta:

| Status | Classe | Quando |
|---|---|---|
| `415` | `permanente` | o mime da parte não está na tabela acima |
| `413` | `permanente` | acima do teto **da categoria**. A mensagem de erro diz o teto em vigor e diz que ele é do gateway |
| `400` | `permanente` | não veio a parte `arquivo`, ou o corpo não é multipart |

`401`, `403`, `404` e `503` seguem a mesma tabela do envio.

### Baixar bytes: `GET /v1/media/{id}?instancia={slug}&mime_do_payload=…`

Devolve os **bytes** no corpo e **os dois mimes** em cabeçalhos:

```
X-Zapgw-Mime-Do-Payload: audio/ogg; codecs=opus   ecoado do que VOCÊ mandou na query
X-Zapgw-Mime-Do-Get:     audio/ogg                o que o GET /{media_id} da Meta reporta
Content-Type:            application/octet-stream sempre — leia os dois acima
```

O `mime_do_payload` é opcional e vem **de você**, não de um registro nosso: o gateway não guarda
mensagem, então quem tem esse valor é quem recebeu o evento (`midia_mime_payload`). Se você não
mandar, o cabeçalho vem **ausente** — o gateway não copia o outro para o lugar dele, porque isso
seria inventar dado com cara de verdade. Um `mime_do_payload` que não seja um mime válido é recusado
com `400`.

O `Content-Type` é `application/octet-stream` **de propósito**: pôr um dos dois mimes ali seria o
gateway escolhendo por você, e quem lesse só o `Content-Type` levaria a escolha errada sem nunca ver
que havia duas. A escolha é sua, e a próxima seção diz por quê.

#### Erros do download

Mesma taxonomia do envio — **decida pela `classe`, nunca pelo status**. Esta rota fala com a Meta
duas vezes (descrever a mídia, depois buscar os bytes), e qualquer uma das duas pode falhar:

| Status | Classe | Quando |
|---|---|---|
| `400` | `permanente` | `mime_do_payload` que não é um mime válido (mande o `midia_mime_payload` **exatamente** como veio no evento); **ou** o `{id}` da mídia tem forma inválida e o pedido nem saiu do gateway; **ou** a Meta recusou o id — mídia que não existe, que não é da sua conta, ou que já expirou |
| `401` | `config` | seu `Authorization` está ausente ou é inválido |
| `403` | `config` | a instância pedida não é sua |
| `404` | `config` | a **instância** não existe. Repare: id de mídia inexistente **não** cai aqui, cai no `400` acima, porque quem o recusa é a Meta |
| `502` | `desconhecido` | a Meta não devolveu um endereço utilizável para essa mídia, ou o gateway não conseguiu falar com ela |
| `503` | `retentavel` | a instância está pausada, o gateway não leu o próprio armazenamento, **ou** a Meta devolveu erro retentável (5xx, timeout, ou throttling reconhecido pelo **código** da Meta — não pelo status; ver a nota em *POST /v1/messages → Erros*) |

> ⚠️ **O erro pode chegar no meio dos bytes, e aí não há corpo de erro nenhum.** Os bytes viajam em
> streaming: se a conexão com a Meta cair **depois** de o `200` e os cabeçalhos já terem saído, o
> status já foi `200` e não há JSON de erro para você classificar — a resposta simplesmente **acaba
> antes**. A resposta não traz `Content-Length` (é `chunked`), então um cliente HTTP correto acusa
> leitura incompleta; **não engula essa exceção**, e não grave o arquivo parcial como se estivesse
> inteiro. Repetir o `GET` é seguro: baixar mídia não muda nada do lado da Meta.

---

## Perfil do negócio — `GET/POST /v1/perfil` (2026-08-20)

**`GET/POST https://zapgw.exemplo.com.br:8443/v1/perfil`** · `Authorization: Bearer <seu token>`

É o que o seu cliente vê ao tocar no **nome do negócio**, dentro do WhatsApp: descrição, endereço,
e-mail, site, ramo. Até esta rota, isso só se mudava pelo painel da Meta, na mão. Rota da **LAN**,
como as outras de instância — e exclusiva de WhatsApp, ver *Rotas que recusam `400` numa instância
de Instagram*, mais abaixo.

### Ler — `GET /v1/perfil?instancia=lojinha`

```jsonc
// resposta 200
{ "instancia": "lojinha",
  "about": "Loja de roupas femininas",
  "vertical": "RETAIL" }
```

**O gateway devolve exatamente o que a Meta mandou — nunca inventa um campo ausente.** Se `email` ou
`websites` nunca foram configurados, eles **não aparecem** no corpo, em vez de virem como `""` ou
`[]` — um campo ausente e um campo vazio são fatos diferentes, e esta rota não apaga essa diferença.
`websites` é uma lista (até 2 endereços, segundo a Meta); os demais são texto solto.

### Escrever — `POST /v1/perfil`

```jsonc
// pedido — SO os campos que voce quer trocar
{ "instancia": "lojinha",
  "about": "Nova descricao curta" }

// resposta 200
{ "instancia": "lojinha",
  "gravado": { "about": "Nova descricao curta" } }
```

🔴 **CAMPO AUSENTE NO PEDIDO NÃO É CAMPO VAZIO — é a instrução explícita de "não mexa".** O `POST`
manda à Meta **só** os campos que vieram no seu corpo; um campo que você não mandou continua com o
valor que já estava lá. Mandar `"description": ""` de propósito **apaga** a descrição — é diferente
de não mandar `description`, que a preserva. Se você quer apagar um valor, mande o campo com
`""` (ou `"websites": []` para limpar a lista de sites) — é um pedido válido e sai exatamente assim
para a Meta; a única coisa que este gateway nunca faz é inventar essa troca sozinho.

Os campos aceitos: `about` (até 139 caracteres, segundo a Meta), `description` (até 512), `address`,
`email`, `websites` (até 2), `vertical`, e `profile_picture_handle` (o `media_id` que
`POST /v1/media` devolveu, para trocar a foto do perfil pelo conteúdo daquele upload).

⚠️ **Este gateway não confere os tetos de caracteres nem a quantidade de sites.** Quem valida é a
Meta, e ela **explica** o que recusou — o mesmo `explicacao_meta`/`rastro_meta` da T-153, na
próxima seção. Duplicar o número aqui só criaria uma segunda fonte que divergiria da Meta no dia em
que ela mudar o próprio limite.

### Erros

Mesma taxonomia das outras rotas de instância — **decida pela `classe`, nunca pelo status**:

| Status | Classe | Quando |
|---|---|---|
| `400` | `permanente` | falta `instancia` no `GET`, falta `instancia` no corpo do `POST`, ou o corpo não é JSON |
| `400` | `retentavel` | o corpo do `POST` não chegou inteiro (sua conexão caiu no meio) |
| `401` | `config` | seu `Authorization` está ausente ou é inválido |
| `403` | `config` | a instância pedida não é sua |
| `404` | `config` | a instância pedida não existe |
| `503` | `retentavel` | a instância está pausada, ou o gateway não falou com o próprio armazenamento |
| `502` | `config` | a credencial que o **gateway** guarda para essa instância foi recusada pela Meta |
| `400`/`502` | vem da Meta | a Meta recusou um campo (teto de caracteres, `vertical` desconhecido, `profile_picture_handle` que não é imagem…) — a resposta carrega `mensagem`, e quando a Meta manda, `detalhe_meta`/`explicacao_meta`/`rastro_meta` (T-141/T-153) |
| `502` | `desconhecido` | o gateway não obteve resposta utilizável da Meta; repetir é seguro (ler e escrever perfil não têm efeito colateral por si só além do próprio campo pedido) |

✅ **VERIFICADO CONTRA A META DE VERDADE (T-157, 2026-08-20, `v0.59.0` no ar):** o identificador da
sua instância que este endpoint da Graph API usa é `phone_number_id` — confirmado por uma chamada
real (`GET /v1/perfil?instancia=tenant-one` devolveu `200` com o perfil completo; o identificador
errado teria devolvido `404`). Isso era dúvida na primeira versão desta rota (as referências de
terceiro consultadas divergiam entre `phone_number_id` e `waba_id`) e deixou de ser — nada no
comportamento mudou, só o grau de certeza sobre o nó.

---

## Instagram — a primeira fatia (2026-07-30)

Tudo neste documento, até aqui, descreve uma instância **WhatsApp**. O gateway também atende
**Instagram**, numa instância de **outro tipo** — e as duas nunca se misturam: uma instância é
WhatsApp *ou* Instagram, nunca as duas, e você sabe qual é a sua porque foi você (ou o dono) quem
escolheu o tipo na criação.

**O que existe hoje, e nada além disso:**

- **Receber e responder mensagem de texto.** Sem mídia, sem template, sem botão, sem reação, sem
  resposta a story, sem marcação de leitura. `POST /v1/messages` numa instância Instagram só aceita
  `tipo:"texto"` — qualquer outro tipo leva `400`.
- **Sem `POST /v1/cadastro` para Instagram.** A instância WhatsApp pode nascer só com o slug e ser
  configurada depois, por API, pelo consumidor (seção *Cadastrar a SUA Meta*, acima) — Instagram
  **não tem esse caminho** nesta fatia. Toda instância Instagram é criada **manual e completa** pelo
  dono, com a identificação já pronta. Se você ainda não recebeu uma, ela não existe.

### O que muda no endereçamento

Uma instância Instagram não tem `waba_id`, `phone_number_id` nem `numero_exibido` — eles não
existem no Instagram. No lugar deles:

| WhatsApp | Instagram | O que é |
|---|---|---|
| `waba_id` + `phone_number_id` | **IG ID** (`ig_id`) | identifica a CONTA — quem recebeu o webhook, quem envia a mensagem |
| um telefone | **IGSID** (Instagram-scoped ID) | identifica QUEM VOCÊ CONVERSA — nunca é um telefone, nunca tente formatá-lo como um |

O `IGSID` é o valor que você recebe em `eventos[].de_canonico` (e em `eventos[].de_cru` — os dois
vêm **idênticos**: um IGSID não passa pela normalização de telefone brasileiro que o WhatsApp usa,
porque ele não é um telefone) e é o mesmo valor que você manda de volta em `para`, ao enviar.

### Receber uma mensagem

O evento chega no **mesmo formato** que uma mensagem de WhatsApp (`"tipo":"mensagem"`) — você não
precisa aprender um vocabulário novo. A diferença é o que vem preenchido:

```json
{
  "tipo": "mensagem",
  "id": "msg:IGMID...",
  "wa_message_id": "IGMID...",
  "sub_tipo": "text",
  "de_cru": "179...IGSID",
  "de_canonico": "179...IGSID",
  "texto": "oi, vocês entregam hoje?"
}
```

`wa_message_id` carrega o id que a Meta deu à mensagem no Instagram (o `mid`) — o nome do campo é
herdado do WhatsApp de propósito: os dois são "o identificador que a Meta deu a esta mensagem", e
você não precisa de um nome de campo por produto para reconhecer isso.

Esta fatia **só modela mensagem de texto**. Uma mensagem com anexo (imagem, áudio, resposta a
story, reação) chega no `cru`, mas **não vira um evento em `eventos`** — o `parse_error` do
envelope acusa que algo ficou de fora. Se você precisar desses tipos, peça — o envelope só cresce.

🔴 **A mensagem que VOCÊ envia também chega de volta pelo webhook — como ECO — e ela NUNCA vira
evento em `eventos` (T-105).** A Meta reenvia, no mesmo `messaging[]`, uma notificação de toda
mensagem que o seu próprio negócio manda (`message.is_echo: true`,
[developers.facebook.com/docs/messenger-platform/instagram/features/webhook/](https://developers.facebook.com/docs/messenger-platform/instagram/features/webhook/)),
com o remetente sendo **o seu próprio `ig_id`**, não um IGSID de cliente. O gateway filtra esse
item: ele **não vira `tipo:"mensagem"`**, mas continua presente no `cru` (base64), inteiro, como
todo o resto do lote. Isso existe porque, sem o filtro, um sistema automático que responde a toda
mensagem recebida acaba respondendo **à própria resposta que ele mesmo mandou** — a Meta recusa
esse envio (ela não entrega mensagem de um negócio para ele mesmo), e cada resposta sua vira uma
tentativa de envio fadada a falhar. Você **não precisa filtrar isso do seu lado**: se um lote trouxe
só ecos, `eventos` chega vazio (`[]`), nunca com o eco disfarçado de mensagem de cliente.

### Enviar uma mensagem

`POST /v1/messages`, exatamente como no WhatsApp, com `instancia` apontando para a sua instância
Instagram e `para` = o IGSID de quem vai receber:

```json
{"instancia":"<seu-slug-instagram>","para":"<IGSID>","tipo":"texto","texto":"Oi! Fechamos às 18h."}
```

A resposta de sucesso é a mesma forma do WhatsApp — `{"wa_message_id":"<id>"}` — pelo mesmo motivo
do campo de entrada: um nome só para "a Meta aceitou e deu um id a isto", nos dois produtos.

🔴 **A janela de 24 horas — e ela é MAIS ESTREITA do que no WhatsApp.** Você só pode mandar mensagem
para quem te escreveu **nas últimas 24 horas** (estendida a **7 dias** se você usar a tag
`human_agent` — este gateway não monta essa tag por você nesta fatia). No WhatsApp, fora da janela
ainda dá para iniciar conversa com um **template** aprovado; **Instagram não tem template**, então
fora da janela **não há o que fazer** além de esperar o cliente escrever de novo. Se o seu envio for
recusado por causa da janela, a Meta responde com um erro `permanente` ou `config` — o gateway
repassa a mensagem dela tal como veio, sem inventar uma tradução que não foi conferida na fonte.

### Provar o canal — `POST /v1/fumaca` / `zapgw fumaca`

Funciona como no WhatsApp — a instância nasce **pausada** e só um envio de teste aceito a ativa —
mas com duas diferenças honestas.

**Primeira: o `destino` do teste de fumaça tem de ser um IGSID que te mandou mensagem nas últimas
24 horas.** O gateway não verifica isso antes de tentar (não há como, sem adivinhar o que a Meta vai
responder); ele tenta o envio de verdade e deixa a Meta decidir. Se ela recusar por causa da janela
fechada, o erro devolvido diz isso explicitamente ao lado do que a Meta respondeu — você não fica só
com um código de erro genérico para decifrar.

**Segunda (T-104): o passo "a Graph API aceita o token", que no WhatsApp roda ANTES do envio de
teste, não existe para Instagram.** No WhatsApp esse passo é um `GET` que confirma o token sem
gastar o envio de teste; para Instagram não há, **medida**, uma chamada equivalente nesse host que
faça a mesma pergunta sem efeito colateral — inventar uma seria arriscar uma resposta que engana.
Isso significa que, numa instância Instagram, um token revogado só é acusado **no próprio envio de
teste** (o mesmo `erro.classe = "config"` que você já esperava, só um passo mais tarde) — nunca muda
o que a resposta final diz, só quando a Meta é consultada.

### O que NÃO existe, e não é lacuna a perguntar sobre

- Template, botão, mídia, reação, localização, resposta a story, marcação de leitura.
- Qualidade do número / tier de mensagens — conceito de WhatsApp, sem equivalente modelado aqui.
- `GET /v1/estado` continua funcionando (ele é genérico, por slug), mas os blocos específicos de
  WhatsApp (`token_meta`, `numero_na_meta`) sempre respondem `nao_se_aplica` — **explicitamente**,
  nunca vazios nem ausentes (T-099) — numa instância Instagram. Ver a tabela logo abaixo.
- 🔴 **A EXCEÇÃO é `token_instagram` (T-098): ele é o bloco que SÓ existe do lado do Instagram** — o
  token de longa duração vence em 60 dias, sem equivalente no WhatsApp (System User não expira
  assim). É por ali que você vigia se o token deste canal ainda vai funcionar amanhã — ver a seção
  própria do bloco, acima.

### Rotas que recusam `400` numa instância de Instagram (T-111)

Seis rotas usam um campo que só existe numa instância **WhatsApp** — `phone_number_id` ou
`waba_id` — e por isso **recusam com `400`, classe `config`**, numa instância Instagram, antes de
tentar qualquer coisa com a Meta. A mensagem sempre nomeia o tipo recusado:

| Rota | Por que não se aplica ao Instagram |
|---|---|
| `POST /v1/leituras` | usa `phone_number_id` para marcar a mensagem como lida — sem equivalente modelado aqui |
| `POST /v1/media` (upload **e** download) | idem — e esta fatia do Instagram não envia mídia nenhuma (ver acima) |
| `POST /v1/templates` **e** `GET /v1/templates` | usa `waba_id` — Instagram não tem template nesta fatia |
| `POST /v1/cadastro` | grava `waba_id`/`phone_number_id`/`numero_exibido` — **instância de Instagram é configurada por quem opera o gateway**, não por esta rota (é a mesma regra da seção "Sem `POST /v1/cadastro` para Instagram", acima) |
| `POST /v1/bloqueios`, `DELETE /v1/bloqueios` **e** `GET /v1/bloqueios` | usa `phone_number_id` — bloqueio de usuários é exclusivo da Cloud API do WhatsApp (2026-08-20) |
| `GET /v1/perfil` **e** `POST /v1/perfil` | usa `phone_number_id` (confirmado por medição — T-157, 2026-08-20) — perfil de negócio é exclusivo da Cloud API do WhatsApp (2026-08-20) |

`GET /v1/instances/{slug}/health` é a **única exceção**, e por decisão deliberada: sendo rota de
**leitura**, ela nunca recusa — um `400` ali quebraria a vigilância em laço de quem monitora o
canal. Numa instância Instagram ela responde `200` com `"veredito":"nao_se_aplica"` e **sem chamar
a Meta**: não existe, em `graph.instagram.com`, um equivalente ao `GET /{phone_number_id}` que
confirme o token sem enviar mensagem — inventar um por analogia arriscaria uma resposta que engana.

#### Qual bloco de `GET /v1/estado` se aplica a qual tipo de instância

| bloco | WhatsApp | Instagram |
|---|---|---|
| `estado` / `pausada` / `versao` / `gerado_em` / `carimbos_desde` / `contadores` / `serie_7_dias` / `serie_diaria` | sim | sim — genéricos, independem do produto Meta |
| `tipo` | sim — sempre `"whatsapp"` | sim — sempre `"instagram"` |
| `ig_id` | **sempre `nao_se_aplica`** (T-107) — identificador do Instagram, o WhatsApp não tem | sim — o Instagram-scoped Business Account ID desta instância, o mesmo valor de `zapgw instancia mostrar` |
| `certificado_do_callback` | sim | sim — é o TLS do **seu** endpoint, o mesmo nos dois produtos |
| `token_meta` | sim, medido a cada 5 min | **sempre `nao_se_aplica`** (T-099) — a checagem mede por `phone_number_id`, que Instagram nunca tem |
| `numero_na_meta` (`qualidade`, `limite_de_mensagens`) | sim, medido/empurrado | **sempre `nao_se_aplica`** (T-099) — qualidade e tier são conceitos do WhatsApp Business Number |
| `token_instagram` | **sempre `nao_se_aplica`** (T-098) | sim — vencimento em 60 dias, ver a seção própria |
| `entrada` (`via`, `conector`, `ultimo_webhook_em`) | sim | sim — ele é do **gateway**, não do produto Meta: os dois primeiros campos são iguais em toda instância deste gateway (T-120) |

🔴 **`tipo` e `ig_id` entraram na T-107 (2026-07-30) e aparecem SEMPRE, nos dois produtos** — a mesma
cegueira que a T-103 já tinha consertado em `zapgw instancia mostrar`/`listar` continuava aqui: sem
`tipo`, você tinha de deduzir o produto pela ausência dos outros blocos (`token_instagram
nao_se_aplica` etc.), que é adivinhação; e sem `ig_id` você via o bloco `token_instagram` saudável
**sem conseguir confirmar de qual conta Instagram ele fala** — foi exatamente um `ig_id` errado que
causou o defeito descrito na seção de rotação de instância (T-102). `ig_id` é identificador, não
segredo (mesma decisão da T-102): sai o valor, nunca um booleano `cadastrado: sim/não`.

⚠️ **Não conferido nesta fatia:** se a Meta exige App Review/acesso avançado para um App atuar por
contas de terceiros. Isso é onboarding de quem traz a conta (o dono, hoje) — não bloqueia o uso da
API por você, mas não afirme que a Meta nunca vai pedir isso.

---

## As cinco obrigações do consumidor

O zapgw **não guarda estado de mensagem**. Estas cinco coisas ele não pode fazer por você.

### 1. Grave o `cru` ANTES de olhar os `eventos`, e responda depois

`cru` são os bytes exatos que a Meta enviou. `eventos` é **enriquecimento** — pode vir vazio, ou
parcial, com `parse_error` preenchido.

Se você processar `eventos` antes de gravar, e o processamento falhar, você perde uma mensagem que já
tinha em mãos. É o defeito que mais custou nesta rede.

### 2. Deduplique **POR EVENTO**, pelo campo `id` de dentro do corpo

> ⚠️ **Não deduplique pelo header `X-Zapgw-Event-Id`.** Ele carrega o id do **primeiro** evento do
> lote, como conveniência de rastreio. Um lote cujo primeiro evento você já viu, mas com eventos
> novos depois, seria **descartado inteiro** — em silêncio.

O `id` é **determinístico**, derivado do conteúdo:

| Evento | Formato do `id` |
|---|---|
| mensagem | `msg:{wa_message_id}` |
| status | `status:{wa_message_id}:{status}` |
| template_status | `template_status:{message_template_id}:{event}:{entry.time}` |
| template_categoria | `template_categoria:{message_template_id}:{previous_category}:{new_category}:{entry.time}` |
| qualidade_do_numero | `qualidade_do_numero:{display_phone_number}:{event}:{old_limit}:{current_limit}:{entry.time}` |
| alerta_de_conta | `alerta_de_conta:{entity_id}:{alert_type}:{alert_severity}:{alert_status}:{entry.time}` |

A chave do status é composta **de propósito**: `sent`, `delivered` e `read` chegam com o **mesmo**
`wa_message_id`. Deduplicar só pelo `wa_message_id` descartaria dois dos três.

**A do `template_status` inclui o TEMPO pela razão oposta, e ela é menos óbvia:** ali os estados
**se repetem** — o mesmo template pode ser `APPROVED` várias vezes ao longo da vida (aprovado,
editado, volta a pendente, aprovado de novo). Sem o tempo, a segunda aprovação teria o id da
primeira e o seu dedup a descartaria. Ver a seção do evento, mais acima.

**A do `template_categoria` leva o tempo pelo mesmo motivo e a TRANSIÇÃO por um a mais:** um template
pode **ir e voltar** de categoria. `UTILITY → MARKETING` e `MARKETING → UTILITY` são fatos opostos
(um encarece, o outro barateia) e sem a direção na chave seriam o mesmo `id`; e a **terceira**
transição (`UTILITY → MARKETING` de novo) é a mesma direção da primeira, então só o tempo a separa —
e é justamente ela que reabre a janela de recurso.

**As duas últimas seguem a mesma lógica, e existem porque esses webhooks NÃO TÊM ID.** Não há um
`{wamid}` nem um `{message_template_id}` para ancorar a chave, então ela é montada com o que
distingue um evento do vizinho: no `qualidade_do_numero`, o número + o `event` + a transição de
limite; no `alerta_de_conta`, a entidade + o tipo + a severidade + o estado. **Um payload em que
NADA disso seja legível não vira evento** — a chave colidiria com qualquer outro igualmente vazio do
mesmo lote, e o seu dedup apagaria os dois. O `cru` chega assim mesmo, e o `parse_error` acusa.

**Trate o `id` como opaco. Não faça parse dele.** O formato acima é informativo; a garantia é que o
mesmo evento produz sempre o mesmo `id`, e eventos diferentes produzem `id`s diferentes.

Determinismo é o que faz a reentrega legítima da Meta e um reenvio malicioso caírem no **mesmo**
dedup.

> ⚠️ **`SELECT` e depois `INSERT` NÃO é deduplicação. A garantia tem de ser ATÔMICA.**
>
> ```python
> if Mensagem.objects.filter(wa_message_id=wa_id).exists():   # ← NÃO é dedup
>     return
> ```
>
> Esse padrão é o que quase todo mundo escreve lendo a frase "deduplique pelo `id`", e ele **falha
> exatamente quando é preciso**: duas entregas do MESMO evento chegando ao mesmo tempo passam as
> duas pelo `exists()`, e as duas inserem.
>
> **E entrega simultânea do mesmo evento não é hipótese — é o comportamento normal da Meta.** Medido
> no nosso access log, ela reentrega **5 vezes em 9 segundos** quando não recebe um `200` a tempo:
> `:02 · :04 · :05 · :07 · :11`. Se o seu processamento levar mais que o intervalo entre duas
> tentativas, elas se sobrepõem. Com mais de um worker (processo, thread ou pod), as duas rodam em
> paralelo.
>
> **O jeito certo é o banco recusar:**
>
> ```sql
> CREATE UNIQUE INDEX CONCURRENTLY uniq_evento_id ON eventos (evento_id);
> ```
>
> …e tratar a violação de unicidade como **sucesso** (`200`), não como erro — é o segundo entregador
> descobrindo que o primeiro ganhou. Um `INSERT … ON CONFLICT DO NOTHING` (ou o equivalente do seu
> banco) resolve na mesma instrução.
>
> **Por que isso importa mais do que uma linha repetida:** a duplicata não custa uma linha a mais —
> ela **re-executa os efeitos colaterais**. Num consumidor real desta rede (medido em 2026-07-26),
> um evento duplicado criaria a mensagem de novo na tela, reencaminharia para o Telegram e
> **dispararia outra resposta automática para a cliente** — além de queimar cota da Meta, que é
> limitada. O custo aparece no celular de uma pessoa, não no banco.
>
> **Janela de anti-repetição por tempo não substitui isto.** Um sistema desta rede tinha uma guarda de 60 s
> (mesma mensagem, mesmo destinatário) que absorveria a rajada de 9 s da Meta — **mas não a
> reentrega de +5 min nem a de +1 h35**, que é justamente quando o evento volta.

#### E quando o lote não tem evento nenhum (`"eventos": []`)? — a chave é OUTRA (2026-07-28)

A obrigação 1 manda gravar o `cru` **sempre**, inclusive quando o lote chega sem evento nenhum —
`"eventos": []` no fio desde 2026-07-28 (era `null` antes; veja *Webhook de CONTA*, que também traz a
forma segura de iterar). Acontece com webhook de conta não modelado, com `parse_error` preenchido, e com qualquer lote
que o gateway entregou sem conseguir enriquecer. Só que aí não existe `eventos[].id`, e o parágrafo
acima não diz com que chave gravar. **Esta seção existe
porque a resposta intuitiva está errada**, e a orientação errada chegou a ser dada por escrito, no
canal, por quem mantém este gateway — o consumidor não a seguiu, foi ao código e a derrubou.

**São duas perguntas diferentes, com respostas OPOSTAS, e é colar uma na outra que produz o bug:**

| Pergunta | Sobre quais bytes | Por quê |
|---|---|---|
| **Verificar a assinatura** | os bytes **do corpo do POST, exatos, como chegaram no fio** | é sobre eles que o HMAC foi calculado — veja a obrigação 3, logo abaixo. **Nada aqui muda isso.** |
| **Deduplicar um lote sem evento utilizável** | o **`cru`**, decodificado de dentro do envelope | o corpo do envelope **muda entre reentregas do mesmo evento**; o `cru` não |

**Nunca hasheie o corpo do envelope para deduplicar.** O envelope é **remontado a cada entrega**:
`recebido_em` vem de uma leitura do relógio feita **naquela** entrega, e o JSON é serializado logo em
seguida. Duas entregas do
**mesmo** evento da Meta produzem, portanto, corpos **diferentes** — hash diferente, `UNIQUE`
diferente, **linha duplicada**: exatamente o que o dedup existe para impedir. Isso não é intermitente
nem raro; falha **por construção**, em toda reentrega.

**Faça assim:** decodifique o base64 do campo `cru` e use o hash desses bytes como chave única
(`sha256` serve). Escolha uma forma — bytes decodificados ou a string base64 como veio — e **não
mude depois**, senão a mesma entrega ganha duas chaves. Trate a violação de unicidade como sucesso,
pela mesma regra do bloco acima.

> ⚠️ **O header `X-Zapgw-Event-Id` não serve de plano B aqui: ele só existe quando há evento.**
> O gateway só o escreve quando o lote produziu pelo menos um evento.
> Num lote sem evento o header simplesmente **não vem** — e é justamente por isso que este
> caso precisa de chave própria. (Mesmo quando ele vem, não deduplique por ele: veja o aviso no
> começo desta obrigação.)

**A premissa que sobra, dita em voz alta em vez de escondida:** hashear o `cru` assume que a Meta
**reentrega os mesmos bytes**. Isso **não é garantia publicada por ela** — este projeto proíbe
afirmar como fato o que não foi conferido na fonte, e esta não foi. É a melhor opção disponível, e é
**estritamente melhor** que o envelope: o `cru` falha só se a Meta mudar os bytes entre reentregas
(não observado); o envelope falha **sempre**. Se um dia aparecer reentrega com bytes diferentes, o
sintoma é uma linha crua duplicada no seu banco — barato, e o oposto do que acontece hoje com o
envelope, que já é duplicata garantida.

### 3. Verifique a assinatura, e verifique o timestamp

**A assinatura cobre o timestamp E o corpo.** A conta, literal:

```
mensagem_assinada = <X-Zapgw-Timestamp em ASCII> + "." + <corpo do POST, bytes exatos>

X-Zapgw-Signature = "sha256=" + hex_minúsculo(
                        HMAC_SHA256(chave  = segredo_de_entrega_da_sua_instância,
                                    dados  = mensagem_assinada))
```

Três detalhes que decidem se a sua conta vai fechar:

- O **corpo é o que chegou no fio, byte a byte** — nunca JSON reserializado. Um `json.dumps` no meio
  reordena chaves e muda espaços, e a assinatura deixa de fechar sem que nada explique por quê. Leia
  os bytes crus antes de qualquer parse.
- O **timestamp é a string exata do header**, não um inteiro re-formatado. Hoje ele é um unix em
  segundos, decimal, sem sinal e sem zeros à esquerda, mas concatene **o texto que veio** — assim a
  sua conta continua fechando mesmo que o formato mude.
- O separador é um **ponto** (`.`), entre os dois. Ele existe para a fronteira entre timestamp e
  corpo ser inequívoca: em concatenação crua, `("1769000000", "0x")` e `("17690000000", "x")` dão os
  mesmos bytes assinados, e portanto a mesma assinatura.

#### O vetor de teste — confira a sua implementação contra ele ANTES de ligar qualquer coisa

Este é o vetor **congelado** do gateway, reproduzido aqui inteiro para você não precisar de nada além
desta página. **Se a sua implementação produzir esta assinatura com estes valores, ela está certa; se
não produzir, o problema é dela e não do gateway.**

```
segredo_entrega = segredo-de-brinquedo-do-vetor-de-teste-NAO-E-CREDENCIAL
timestamp       = 1769000000
corpo (129 bytes, exatamente estes, sem quebra de linha no fim):
{"instancia":"lojinha","recebido_em":"2026-07-26T12:00:00Z","texto":"Olá, João — 50% de ação no caminho C:\\tmp\\nota.pdf"}

assinatura esperada:
sha256=f685419474eed78cfd0458e16057a70317556c154d1d78f06d28f47c87fe35d3
```

🔴 **Leia o corpo como BYTES, não como JSON.** As barras invertidas ali são **duas de verdade**
(`C:` + `\` + `\` + `tmp`), tal como aparecem no fio; se a sua linguagem exige escapá-las de novo para
escrever a string literal, escape — o que entra no HMAC são os 129 bytes acima. O `segredo_entrega`
deste vetor é de **brinquedo**: valor inventado, público, que não abre nada em lugar nenhum. O de
verdade é sorteado por instância e é o item 4 do seu pacote de entrega.

**O corpo carrega de propósito um acento, um travessão (3 bytes em UTF-8) e as barras invertidas** —
é exatamente onde reimplementação quebra (codificação UTF-8 e escape de JSON), e um vetor só-ASCII
passaria verde numa implementação errada. Ele é uma **cadeia de bytes opaca**, não o esquema do
envelope: campo novo no envelope não muda o vetor, e o vetor não descreve o formato da entrega.

Compare em **tempo constante** (`hmac.Equal`, `hash_equals`, `crypto.timingSafeEqual`), nunca com
`==`.

Rejeite `X-Zapgw-Timestamp` fora de uma janela de tolerância sua (alguns minutos é o usual, e o
tamanho é decisão sua — o gateway não impõe nenhuma). **Agora essa rejeição vale alguma coisa:** como
o timestamp está dentro da assinatura, quem capturar uma entrega não consegue reenviá-la com um
timestamp novo sem invalidá-la, e sem o seu segredo de entrega não consegue reassinar.

> ⚠️ **MUDANÇA QUE QUEBRA (2026-07-26).** Até aqui a assinatura cobria **só o corpo**, e o
> timestamp viajava fora dela — a "tolerância" recomendada acima não protegia de nada, porque bastava
> reenviar a entrega capturada com um timestamp novo. Quem já validava do jeito antigo
> (`HMAC(corpo)`) **passa a receber assinatura inválida** e precisa adotar a fórmula acima.
> **Está no ar desde a `v0.6.0` (2026-07-26). Implemente a fórmula acima e só ela** — a antiga não
> existe mais em nenhum gateway em operação.
> *Este parágrafo dizia, até 2026-07-28, que a mudança "ainda não saiu numa versão numerada" e que a
> última publicada era a **v0.5.0**, que assinava só o corpo — e mandava conferir contra o binário em
> execução. **Era falso**, e da pior espécie: quem escrevesse o verificador por ele implementaria
> `HMAC(corpo)`, toda entrega falharia a conferência, a regra do próprio contrato manda responder
> `5xx` nesse caso, e a Meta reentregaria por 36 h até a **perda definitiva** — sem ninguém a quem
> perguntar. Achado por auditoria feita com o critério de "um terceiro sem canal consegue integrar?".*
> A implementação de referência, se você quiser conferir a sua contra ela, é `SignDelivery` /
> o próprio vetor acima.

**Prova ponta a ponta, opcional:** `X-Hub-Signature-256` traz a assinatura original da Meta sobre o
`cru` (após decodificar o base64). Se você tiver o `app_secret`, pode refazê-la e provar a origem sem
depender do gateway. O gateway já a verificou — este repasse existe para quem quiser a garantia sem
intermediário.

### 4. Guarde contra regressão de status

`sent` → `delivered` → `read` chegam separados e **podem chegar fora de ordem**. O gateway não guarda
estado e não sabe o que já passou. **Nunca deixe um estado anterior sobrescrever um posterior.**

🔴 **E NÃO ordene pelo `timestamp` da Meta — ordene pela PRECEDÊNCIA do estado** (`sent` < `delivered`
< `read` < `failed`), guardando o mais avançado que já chegou.

*Esta obrigação dizia "use o `timestamp` do evento para ordenar" até 2026-07-28, e **contradizia
frontalmente o aviso 🔴 deste mesmo documento** (ver a seção de status): `sent` e `delivered` da mesma
mensagem chegam com o **mesmo** `timestamp` — medido em tráfego real, com os dois fixtures congelados
(`status_sent_com_pricing.json` e `status_delivered.json`, mesmo `wa_message_id`, `timestamp`
`1785072102` nos dois). Quem seguisse esta seção construiria exatamente o defeito que o aviso existe
para impedir, e ele é **invisível**: nenhum erro em lugar nenhum, só a tela mostrando `delivered`
antes de `sent` metade das vezes. A contradição estava na seção NORMATIVA, que é de onde um
integrador tira a checklist — por isso valia mais que o aviso.*

### 5. Ao REENVIAR mídia, use o `mime_do_payload` — nunca o `mime_do_get`

**Esta é obrigação sua, e o gateway não pode assumi-la.** A Meta reporta a **mesma** mídia com dois
mimes diferentes:

| Onde | Exemplo |
|---|---|
| no evento da mensagem (`midia_mime_payload`) | `audio/ogg; codecs=opus` |
| no `GET /{media_id}` (`X-Zapgw-Mime-Do-Get`) | `audio/ogg` |

É o `; codecs=opus` que faz o WhatsApp renderizar **nota de voz tocável**. Reenviar com o outro
entrega **anexo de arquivo** — e a mensagem **chega**, sem erro em lugar nenhum: nem na resposta da
Meta, nem no webhook de status. Só o cliente final vê a diferença, e ele não vai te avisar que era
para ser um áudio tocável. **Custo pago em produção nesta rede em 2026-07-20.**

Por isso o gateway devolve **os dois, nomeados, sem normalizar nenhum e sem escolher nenhum**:
normalizar destruiria exatamente o que precisa ser preservado, e escolher tomaria de você a única
decisão que só você pode tomar. Guarde o `midia_mime_payload` que veio no evento; ao subir de volta,
declare-o **inteiro** no `Content-Type` da parte `arquivo`.

---

## O que você responde, e o que isso causa

**O zapgw espelha o seu status para a Meta.** A regra que ele aplica:

| Você responde | A Meta ouve | O que acontece |
|---|---|---|
| **2xx** | `200` | Fim. **A Meta nunca mais reenvia este evento.** |
| **5xx** | `502` | A Meta reenvia. Use quando o reenvio **resolveria** (seu banco caiu). |
| **4xx** | `200` + alarme | A Meta **não** reenvia. Use quando o reenvio **não** resolveria. |
| não responde / demora | `504` | A Meta reenvia, se você voltar a tempo. |
| certificado TLS recusado | `504` + alarme | A Meta reenvia — mas o reenvio leva a **mesma** recusa. Só gente conserta, e o alarme diz isso. |

### ⚠️ Erro de credencial ou de configuração responde **5xx**, nunca 4xx

Esta é a regra que mais custa descobrir sozinho, e a tabela acima explica por quê.

**Assinatura inválida, segredo ausente, instância errada, chave não configurada — responda `5xx`.**
O `4xx` está reservado para **defeito de forma que reenviar não conserta**: corpo grande demais
(`413`), JSON ou base64 inválido (`400`).

O motivo é contraintuitivo e vale escrever inteiro. Quando você responde `4xx`, o gateway diz `200`
à Meta — e **ela nunca mais reenvia**. Se a causa foi divergência de segredo ou de fórmula entre
você e o gateway, **você acabou de destruir a mensagem de um cliente real** por um problema de
configuração que seria consertado em minutos.

E você **não descobre sozinho**: do seu lado o log diz *"recusei um pedido com assinatura inválida"*,
que parece exatamente o comportamento correto. O sintoma aparece semanas depois, como "esse cliente
nunca respondeu".

*"Mas assinatura inválida não é um pedido malicioso?"* — quase nunca, e a assimetria decide:

- Um `POST` **forjado por terceiro** não chega ao espelho. A resposta vai para o atacante, e a Meta
  nem participa. Você não perde nada respondendo `5xx` a ele.
- Uma entrega **real** só falha a conferência por divergência entre os dois lados. Aí o `5xx`
  mantém a janela de 36h aberta e dá tempo de alguém consertar.

Ou seja: `5xx` custa **zero** contra o atacante e **salva** a mensagem legítima. `4xx` não atrapalha
o atacante e **queima** a legítima.

> **Instância errada é o caso mais grave da família.** Se a `callback_url` de outro inquilino
> apontar para você por engano, responder `4xx` destrói **dado de terceiro** em definitivo, e
> responder `2xx` diz à Meta que uma mensagem que não é sua está resolvida — o dono dela nunca a
> recebe. Confira o campo `instancia` do envelope contra a instância que você espera, e responda
> `5xx` se divergir.

**Responder 2xx sem ter gravado é perda definitiva.** Não há segunda chance, não há reprocessamento,
e o gateway não guarda cópia.

**As 36h de reenvio da Meta não são rede de segurança para queda longa** — cobrem um restart de
segundos. Se você ficar fora do ar por horas, o evento se perde e o gateway **nem fica sabendo**.

Responda rápido: o prazo de entrega é configurável por instância, e estourá-lo conta como "não
respondeu".

---

## Limites conhecidos, para não serem descobertos em produção

- **Cada instância precisa da própria URL de webhook na Meta.** Se um mesmo App entregar, num único
  `POST`, eventos de números de instâncias diferentes, o lote é recusado inteiro (`200` + alarme). A
  Meta suporta override de webhook por WABA e por número.
- **Status de template e webhooks de conta não são roteados por número** — a Meta não suporta
  override para eles e não manda `phone_number_id`. Eles são roteados **pelo `waba_id`**, e essa
  guarda existe e está no ar: um webhook de conta cujo `waba_id` não seja o da sua instância **não
  chega até você** (veja *Webhook de CONTA*). O que continua sendo limite é a origem: como a Meta
  entrega todos eles na URL principal do App, é o gateway que precisa decidir de quem é cada um.
- **Corpo acima do teto é recusado com `413`, e o teto é o MESMO nos dois sentidos.** Um único limite
  (`ZAPGW_MAX_CORPO_BYTES`, **default 1 MiB**, ajustável por quem opera) vale para o corpo que a
  **Meta** manda no webhook **e** para o corpo que **você** manda em `POST /v1/messages`,
  `/v1/cadastro`, `/v1/leituras`, `/v1/templates`, `/v1/fumaca`, `/v1/pausa` e `/v1/bloqueios`
  (`POST`/`DELETE`). Acima dele a resposta
  é `413` classe `permanente`, e repetir nunca resolve — encolha o pedido.
  **Mídia não usa este teto**: o upload tem tetos por categoria (a tabela em *Subir bytes*), que são
  maiores.
  🔴 **No sentido de ENTRADA o `413` custa mensagem.** O gateway responde `413` à Meta e **não
  entrega nada**; ela reenvia o mesmo corpo, leva `413` de novo, e quando desiste o evento acabou —
  não há cópia e não há reprocessamento. Do seu lado o único sintoma é **silêncio**: uma mensagem que
  nunca chega. Repetido, isso não é acidente, é o teto baixo demais para o que aquela instância
  recebe; a partir de **3 recusas na mesma instância dentro de 1 h** o gateway grava um `ALARME` para
  quem o opera (no máximo um por hora, para não virar ruído).
- **O catálogo de templates tem um limite de paginação, e ele é ERRO, não corte.** O gateway lê até
  **50 páginas de 100** (~5000 templates); a WABA real
  conferida em 2026-07-25 tem 39. Se algum dia esse limite for atingido, você recebe `502` e
  **nenhuma lista** — nunca uma lista curta com `200`. Uma lista curta silenciosa é o defeito que
  tirou o gateway antigo de produção.
- **`midia_mime_payload` vem cru, com parâmetro.** É o `; codecs=opus` que faz o WhatsApp renderizar
  nota de voz; o mime do `GET /{media_id}` é diferente e mais pobre. Não normalize — e veja a
  obrigação 5, que é onde essa diferença cobra.
- **A lista de mimes e os tetos de upload são do GATEWAY, não da Meta.** Um arquivo recusado com
  `415` ou `413` aqui pode ser perfeitamente aceitável para ela: escolhemos ser conservadores porque
  mandar e descobrir depois custa o upload inteiro. A tabela está em *Subir bytes*, e o caminho para
  um mime que não está nela é **converter o arquivo**.
- **O `timeout_ms` da sua instância não é exposto em rota nenhuma.** Default 5000 ms. A regra prática
  está em *O que você recebe ao ser provisionado*: dê ao seu cliente HTTP um prazo com folga acima
  dele.
- **`de_cru` é o que a Meta mandou; `de_canonico` passou pela canonização do 9º dígito.** Compare
  sempre pelo canônico — a Meta não garante a mesma grafia que você cadastrou.
- **O gateway retém o telefone das suas contrapartes por até 30 dias, em claro (decisão do dono,
  2026-07-30).** É o log de trânsito interno (`zapgw transito` / `zapgw log`, sem rota HTTP — você
  não tem acesso a ele), usado só para responder "esta mensagem passou por aqui?" quando é preciso
  identificar uma mensagem específica. O prazo é `ZAPGW_TTL_TRANSITO_DIAS` (default 30 dias).
  **O que fica gravado:** carimbo, instância, telefone da contraparte, direção, tipo do evento,
  desfecho, o **`wamid`** (o id que a Meta deu à mensagem) e um **HMAC da sua `Idempotency-Key`** —
  a chave em claro nunca entra no banco. **O que NÃO fica:** conteúdo — nem texto, nem nome, nem
  legenda, nem o corpo cru.
  ⚠️ **O `wamid` carrega o telefone do destinatário codificado dentro dele.** Ou seja, o telefone
  do seu cliente fica nesta tabela em **dois** lugares, não um — os dois sob o mesmo prazo de purga.
  *(Esta frase foi acrescentada em 2026-08-18: as duas colunas existiam desde 2026-07-30 e a lista
  acima não as citava. O comportamento não mudou; a descrição é que estava incompleta — e num
  parágrafo sobre retenção de dado pessoal, incompleto é errado.)*
  ⚠️ **São os telefones dos SEUS clientes, numa máquina que não é sua.** Se a sua operação tem
  obrigação própria de retenção ou de eliminação sobre esse dado, conte com esta janela adicional do
  lado do gateway ao dimensioná-la.

---

## Política de depreciação — por PRAZO, não por consenso

Quando um campo é sucedido por outro, o antigo é marcado **OBSOLETO** na seção que o descreve, **com
a data da marcação**. A partir daí vale uma regra só:

> 🔴 **Campo marcado obsoleto continua funcionando por no mínimo SEIS MESES a partir da data da
> marcação.** A remoção, quando acontecer, vira uma entrada em *Mudanças que quebram*, com data e
> instrução de migração.

**Seis meses, e o número é escrito de propósito.** "Algum tempo" não é prazo: ele deixa você lendo
"OBSOLETO" sem saber se pode continuar usando amanhã. Seis meses é mais longo que qualquer ciclo de
integração que este gateway já observou, e curto o bastante para não virar "compatível para sempre" —
que é o destino de todo campo cuja remoção nunca tem data.

**A data mínima está escrita ao lado de cada campo obsoleto.** Hoje são dois:

| Campo | Onde | Marcado obsoleto em | Removível a partir de | Sucessor |
|---|---|---|---|---|
| `dia` | cada entrada de `serie_7_dias` e `serie_diaria` | 2026-07-28 | **2027-01-28** | `dia_utc` |
| `serie_7_dias` | topo de `GET /v1/estado` | 2026-07-29 | **2027-01-29** | `serie_diaria` |

⚠️ **"Removível a partir de" não é "será removido nessa data".** É o **piso**: antes dela a remoção
não acontece, depois dela ela pode acontecer a qualquer momento e só é anunciada aqui. Migre antes do
piso e a data deixa de te interessar.

**Por que prazo e não a forma antiga.** Até 2026-07-28 a regra era *"remove quando todos os
consumidores confirmarem por escrito"* — e ela funcionou uma vez, com `botoes_url`, porque havia
**dois** consumidores, os dois conhecidos e os dois na mesma conversa. Com N integradores de fora,
essa condição **nunca fecha**: sempre falta alguém, e o campo obsoleto vira permanente enquanto o
leitor fica sem saber o que fazer. Prazo tem o defeito oposto, que é o defeito barato: ele pode
remover algo que alguém ainda usava — mas essa pessoa teve seis meses e uma data escrita.

**O que NÃO passa por esta política:** campo **novo**. Ele é aditivo, aparece sozinho e não exige
nada de você.

---

## Mudanças que quebram

**Esta seção é o mecanismo prometido no lugar de versionar o formato.**

A garantia normal é que **o envelope só cresce**: campo nunca some, nunca é reaproveitado com outro
significado, e todo campo novo é omitido quando vazio. É isso que dispensa um número de versão — e é
mais forte que um número, porque número de versão convida o consumidor a ramificar (`if versao >= 3`),
e aí o formato do gateway vira parte da lógica dele.

**Mas às vezes quebra.** Quando quebra, aparece aqui: **data, o que mudou, o que fazer**. Não há
aviso por outro canal, não há lista de anúncios e não há endpoint declarando versão — este arquivo é
versionado no git e tem histórico, e é ele que você lê.

🔴 **Isso te dá uma obrigação, e ela é pequena: releia esta seção antes de subir uma integração
nova, e depois periodicamente.** Não existe ninguém para te procurar. As entradas ficam aqui para
sempre, em ordem de data, e a de cima é a mais antiga — bata o olho na data da última que você já
tinha lido.

### 2026-07-26 · A assinatura de entrega passou a cobrir o TIMESTAMP

**O que mudou:** `X-Zapgw-Signature` era `HMAC(segredo, corpo)`. Passou a ser
`HMAC(segredo, timestamp + "." + corpo)`.

**Por que quebrou em vez de crescer:** o timestamp viajava num header **fora** da assinatura, então a
janela de tolerância que este contrato manda implementar não protegia de nada — bastava reenviar a
entrega capturada com um timestamp novo. Não havia como corrigir isso aditivamente: ou a conta cobre
o timestamp, ou não cobre.

**O que fazer:** refazer a conta conforme a seção *Verifique a assinatura, e verifique o timestamp*.
O vetor de teste congelado está reproduzido na obrigação 3 — refaça-o na
sua linguagem antes de confiar na sua implementação.

**Quando:** feito no mesmo dia em que o único consumidor da época estava implementando a verificação,
de propósito. A janela para corrigir de graça não voltaria.

### 2026-07-28 · Um bloco ilegível deixa de derrubar a mensagem — e `responder_a` passa a faltar sozinho

**O que mudou:** quando `messages[].context` (ou `voice`, dentro de um bloco de mídia) chega da Meta
com um **tipo** diferente do esperado — um texto onde se espera objeto, um número onde se espera
texto, um texto onde se espera booleano —, o gateway agora **descarta só aquele bloco** e entrega a
mensagem. Na prática: a mensagem passa a chegar em `eventos` **sem** `responder_a`, `encaminhada`,
`encaminhada_muitas_vezes` (bloco `context`) ou **sem** `voz` (bloco de mídia).

**O que acontecia antes:** a mensagem **inteira** era descartada da lista `eventos` e contada como
erro de parse. Você continuava recebendo o `cru` e o `parse_error` preenchido — mas este contrato
manda você agir por `eventos` e deduplicar por `eventos[].id`, então, para qualquer consumidor que
siga o contrato, **a mensagem da cliente simplesmente não existia**. Não havia alarme nem contador:
só uma linha no journal do gateway.

**Por que isso é "quebra" e não crescimento:** o envelope não ganhou nem perdeu campo — mudou
**quando** um campo aparece. Um consumidor que tenha escrito, ainda que sem perceber, "se a mensagem
chegou em `eventos` e é uma resposta, então tem `responder_a`" vê agora um caso novo: mensagem
presente, campo ausente. Não dava para fazer aditivamente — ou a mensagem sobrevive ao bloco
ilegível, ou não sobrevive.

**O que fazer:** nada, se você já trata `responder_a` e `voz` como opcionais — e este contrato sempre
mandou tratar (`responder_a` ausente é o caso **normal**, veja a seção dele; `voz` ausente significa
"a Meta não disse", nunca `false`). Se algum ponto do seu código assume presença, é ali que a mudança
aparece. **Ela não pode ser observada com payload bem formado**: nenhum tráfego normal muda de
comportamento.

**Por que é a mudança certa mesmo sendo quebra:** mensagem entregue sem um campo acessório é
infinitamente melhor que mensagem perdida — e perda aqui é **definitiva**, porque o gateway responde
`200` à Meta e ela não reenvia.

**Quando:** feito antes de qualquer perda observada. O defeito era invisível por construção (produz
ausência, não erro), então esperar por evidência era esperar por nada.

### 2026-07-28 · A regra acima passa a valer para TODO bloco de uma mensagem, não só `context` e `voz`

**O que mudou:** a entrada anterior descreve o mesmo comportamento aplicado a **dois** campos. Ele
agora vale para **todos os blocos** de uma mensagem — `text`, `button`, `interactive`, `audio`,
`image`, `video`, `document`, `sticker`, `reaction`, `location`, `errors`, `context`, além de `from`,
`type` e `timestamp`. Um bloco que chegue da Meta com **tipo** diferente do esperado é descartado
sozinho, e a mensagem chega em `eventos` sem ele.

**O que acontecia antes:** exatamente o mesmo defeito da entrada anterior, nos outros treze campos —
medido, não suposto: `"text":"oi"`, `"audio":"x"`, `"interactive":"x"`, `"reaction":"x"` e
`"button":"x"` faziam a mensagem **inteira** sumir de `eventos`. **`text` é o caso que importa
para você:** é o tipo de mensagem mais comum de todos, e um `text` de forma inesperada apagava a
mensagem mais banal do sistema.

**Por que é quebra e não crescimento:** mesma razão da entrada anterior — o envelope não ganha nem
perde campo, muda **quando** um campo aparece. Casos novos que você pode observar e não observava:
mensagem com `sub_tipo: "text"` e **sem** `texto`; com `sub_tipo: "audio"` e **sem** `midia_id`; com
`sub_tipo: "reaction"` e **sem** `reacao`; com `sub_tipo: "button"` e **sem** `botao_payload`.

**O que fazer:** trate todo campo do envelope como opcional, inclusive os que "sempre vêm" para um
dado `sub_tipo`. Se o seu código faz `evento["texto"]` sem checar, é ali que a mudança aparece.
**Nenhum payload bem formado muda de comportamento** — como na entrada anterior, isto não é
observável em tráfego normal.

**O que NÃO mudou, e é a única exceção:** uma mensagem **sem `id` legível** continua não virando
evento (ela vem no `cru`, com `parse_error`). Sem o `wamid` não há chave de dedup, e um `id` que
chegue como número **não** é convertido em texto — inventar um wamid mandaria você responder a uma
mensagem que não existe. Continuam também descartadas a `reaction` que a Meta manda **sem alvo** e a
`location` que ela manda **sem o objeto** (ou com ele em `null`): aí é a Meta afirmando que o bloco
não existe, o que é diferente de o gateway não saber lê-lo.

**Quando:** mesmo dia da entrada anterior, e é justamente esse o ponto. A primeira rodada fechou dois
campos; esta fecha a classe, e deixa um teste que fica vermelho no dia em que um campo novo nascer
desprotegido. Registrado com o custo no registro de armadilhas do gateway.

### 2026-07-28 · `eventos` passou a sair como `[]` no fio — antes era `null` quando o lote não tinha evento

**O que mudou:** um lote sem evento nenhum vinha com `"eventos": null`. Passou a vir com
`"eventos": []`. O campo continua sempre presente, e nada mais no envelope mudou.

**O que acontecia antes, e por quanto tempo:** desde o primeiro dia do gateway (2026-07-23) até
2026-07-28, todo envelope sem evento levava `null`. E o caso não era raro: o App estava inscrito em
dez campos de webhook e o gateway modelava só parte deles, então **todo webhook de conta não
modelado** chegava assim, de rotina. Este contrato prometia `[]` por escrito o tempo todo, em cinco
lugares — quem seguiu o texto ao pé da letra escreveu `for ev in envelope["eventos"]`, que estoura
`TypeError: 'NoneType' object is not iterable` em Python, ou `if eventos == []`, que nunca casa.

**Por que é "quebra" e não crescimento:** o envelope não ganhou nem perdeu campo — mudou o **tipo**
do valor num caso. Não havia como fazer aditivamente: ou o campo é um array, ou é `null`. Um segundo
campo (`eventos_sempre_array`) seria duas formas de dizer a mesma coisa, que é a dívida que este
projeto acabou de decidir não deixar nascer noutro campo (veja `botoes_url`).

**E é a quebra mais segura possível, o que decidiu a direção:** código que tolerava `null`
(`envelope.get("eventos") or []`, `?? []`) continua funcionando com `[]`, porque `[]` também é falso
em Python e itera zero vezes em qualquer linguagem. Código que quebrava passou a funcionar. O único
padrão que muda de ramo é um `if eventos is None` explícito, e o ramo novo itera zero vezes. **Não há
consumidor a quem `[]` custe** — o detalhe perverso é o inverso: a mudança conserta quem leu o
contrato antigo e não incomoda quem leu o fio.

**O que fazer:** nada, e **não desfaça a defesa que você já tem**. `or []` / `?? []` continua sendo a
forma recomendada aqui.

> 🔴 **Um caso merece conferência, e ele não é hipotético — é o código de um consumidor real desta
> rede.** Se você RAMIFICA por tipo em vez de iterar — `if isinstance(eventos, list): …` /
> `Array.isArray(eventos)` —, o lote sem evento **troca de ramo**: antes o `null` caía no ramo "não
> dá para usar, grave o `cru` sob uma chave derivada dele"; agora `[]` é uma lista e cai no ramo do
> `for`, que roda zero vezes. **Se o ramo do `for` não grava o `cru`, o lote passa a não deixar
> rastro nenhum** — que é exatamente a perda silenciosa que a obrigação 1 existe para impedir, com o
> sinal trocado. Confira que "lista vazia" e "sem eventos utilizáveis" levam ao mesmo destino no seu
> código, e trave com teste. Uma guarda que trate `null` como caso especial (log, alarme, métrica)
> também deixa de disparar — resultado desejado, mas vale saber antes de estranhar o silêncio.

**Quando:** os dois consumidores que existiam à época foram avisados por escrito em **2026-07-28
15:12**, com a instrução de se defenderem **antes** de o fio mudar; os dois responderam no mesmo dia
confirmando a defesa no código deles, e só então a mudança foi feita. A ordem foi essa de propósito:
mudar primeiro e avisar depois transformaria uma correção segura numa quebra real para quem tivesse
ramificado em `is None`. *(Aviso direto assim só é possível enquanto os leitores cabem numa conversa.
Não conte com ele: o mecanismo que vale para você é esta seção.)*

### 2026-07-28 · A mesma regra sobe da MENSAGEM para o LOTE — e um webhook de conta sem `waba_id` legível passa a ser recusado

**O que mudou, e são duas coisas com direções opostas:**

**(1) O lote deixa de morrer junto.** As duas entradas acima valem dentro de uma mensagem. Agora
valem também nos níveis que **contêm** a mensagem: `entry`, `changes`, `value` (`metadata`,
`contacts`, `messages`, `statuses`), o `field` do change, o evento de **status** e o evento de
**status de template**. Um campo de tipo inesperado em qualquer um deles degrada só o que ele
descreve.

**O que acontecia antes** — medido com o parser, não suposto: um `"contacts":"x"` ou um
`"metadata":"x"` apagava **todas as mensagens E todos os status daquele `change`**; um `field` de
tipo inesperado, idem; um `entry.id` de tipo inesperado apagava o `entry` inteiro; e um único campo
estranho num item de `statuses[]` ou no `value` de um `message_template_status_update` apagava aquele
evento. Você recebia o `cru` e o `parse_error`, e `eventos` vinha vazio ou curto.

**Casos novos que você pode observar:** evento de mensagem **sem** `phone_number_id` (bloco
`metadata` ilegível); **sem** `nome_contato` (bloco `contacts` ilegível); evento de status **sem**
`status` ou **sem** `para`/`para_canonico`; evento de template **sem** `nome`/`motivo`. Todos com o
resto do evento intacto, inclusive o `id` de dedup.

**(2) Webhook de CONTA sem `waba_id` legível deixa de ser entregue.** Se `entry.id` chegar num
formato que o gateway não sabe ler, um webhook de conta (`message_template_status_update` e os
outros `field` que não são `messages`) é **recusado inteiro** — nem `eventos`, nem `cru`. Antes ele
era entregue. Do seu lado, isso é indistinguível de "o webhook não chegou"; do nosso, sai `ALARME`
no journal e o contador `conta_descartada` sobe (visível em `GET /v1/estado`).

**Por que não deu para crescer em vez de quebrar:** o `waba_id` é a **única** chave de roteamento que
um webhook de conta carrega, e é por ela que o gateway prova que aquele webhook é da sua instância.
Sem ela legível não há prova. Entregar assim mesmo poria, no seu banco e em definitivo, o conteúdo de
uma conta que ninguém conseguiu atribuir — e o corpo `cru` vai junto na entrega, então filtrar só os
`eventos` não resolveria: seria uma defesa que só parece defesa. Perder um aviso de conta é
recuperável e é **anunciado**; gravar dado alheio no seu banco não é nem uma coisa nem outra.

**O que fazer:** nada, nos dois casos. **(1)** já é a regra que este contrato repete desde a entrada
anterior — trate todo campo como opcional, inclusive num evento de status. **(2)** não tem ação do
seu lado: nenhum payload bem formado é afetado, e se acontecer, quem age somos nós.

**Quando:** mesmo dia das duas entradas acima. As três são a mesma frase aplicada em três alturas da
árvore, e a terceira só existiu porque a segunda mediu os vizinhos em vez de declarar vitória.
Registrado com o custo no registro de armadilhas do gateway.

### 2026-07-28 · O campo `botoes_url` foi REMOVIDO — use `botoes_template`

**O que mudou:** `botoes_url`, o campo de parâmetro de botão de URL de template, **não existe mais**.
Um pedido que ainda o mande recebe `400` com o erro nomeado `botoes_url foi removido; use
botoes_template` e a tradução dentro da própria mensagem. **O botão de URL não foi a lugar nenhum** —
ele é `{"tipo": "url"}` dentro de `botoes_template`, e o componente que sai para a Meta é o mesmo
byte a byte (congelado em teste).

A tradução é mecânica e é toda a migração:

```jsonc
// antes
"botoes_url":      [ {"indice": 0,               "texto": "BR123456789BR"} ]
// depois
"botoes_template": [ {"indice": 0, "tipo":"url", "texto": "BR123456789BR"} ]
```

**Por que não deu para crescer em vez de quebrar — e aqui a resposta honesta é que DAVA, e a gente
escolheu não.** Manter os dois campos custava pouco em código e muito em contrato: duas formas de
dizer a mesma coisa, e um **estado inválido continuando exprimível** — o mesmo botão declarado nos
dois campos, com o mesmo índice. Ele era recusado por uma checagem cruzada, mas *recusado não é
inexprimível*: a guarda dependia de quem mexesse ali lembrar dela, e este projeto usa união
discriminada exatamente para não depender disso. Com um campo só, o índice voltou a ser **estrutural**
— uma lista, um espaço de índices, nada a lembrar.
**E o outro lado da conta é a data:** o campo tinha **dois dias de vida**, e o custo de coordenar uma
remoção com dois consumidores conhecidos nunca vai ser mais baixo do que era hoje. Deixar para depois
transformaria "compatível por enquanto" em "compatível para sempre" — a formulação é do próprio
consumidor que se ofereceu para sair cedo.

**Por que `400` e não silêncio, que é a decisão que mais importa para você:** um campo removido que
fosse simplesmente ignorado pelo desserializador mandaria o template **sem o botão**, com `200` na
resposta. Você não teria sinal nenhum — quem descobriria seria o seu cliente, no celular, num
template incompleto, e a conversa cobrada já teria sido queimada. Os dois erros possíveis têm preços
de ordens diferentes: ignorar custa uma entrega errada e a confiança no número da sua tela; recusar
custa um deploy. **Quando a assimetria é essa, a dúvida se resolve pelo lado barato.**

**O que fazer:** trocar o nome do campo e acrescentar `"tipo": "url"` a cada item, como no bloco
acima. Nada mais muda — nem o índice, nem o texto, nem o componente que a Meta recebe, nem o **hash
de idempotência** (o campo era `omitempty`, então quem já usava `botoes_template` tem o mesmo hash de
antes e nenhum retry dentro do TTL de 72 h vira `422` falso).

**Quando:** a remoção **não teve prazo**. O campo tinha dois dias de vida e existiam dois
consumidores, os dois conhecidos, e ela ficou bloqueada o dia inteiro esperando os dois confirmarem
**por escrito** que não usavam mais o campo — cada um citando a linha do próprio código, não a
memória. Um deles fechou depois de três condições exigidas nesta ordem: PR mesclado, deploy
**conferido** (não presumido) e **botão clicado no aparelho** com o portal abrindo; o outro tinha uso
zero, porque o cliente dele já nasceu no `botoes_template`.

> 🔴 **Essa forma NÃO é mais a política, e a diferença te afeta.** Esperar confirmação de todo mundo
> só fecha quando "todo mundo" é uma lista curta e conhecida — com N integradores de fora, ela nunca
> fecha, e você ficaria lendo "OBSOLETO" sem saber se pode continuar usando. **A política atual é
> prazo**, e está escrita em *Política de depreciação*, mais acima. Este parágrafo é registro do que
> aconteceu naquele dia, não instrução.

> ⚠️ **Um aviso que veio de um consumidor e vale para quem for conferir o próprio código:**
> `botoes_url` pode ser **nome interno seu**, sem relação nenhuma com este campo. Foi o que
> aconteceu com um deles: um `grep` pelo nome no repositório achava **8 ocorrências** de um helper
> local, e quem parasse aí concluiria que ele usava o nosso campo — ele nunca usou. **O que decide
> não é o `grep` pelo nome: é o que sai no JSON do pedido.** Mesma família do `wa_message_id` ×
> `wamid`.

---

**Formato para a próxima entrada:** data · o que mudou · **por que não deu para crescer em vez de
quebrar** · o que fazer · quando. A segunda é a que importa: se der para crescer, cresça — quebrar é
sempre a segunda opção, e esta seção existe para tornar essa escolha visível em vez de conveniente.
