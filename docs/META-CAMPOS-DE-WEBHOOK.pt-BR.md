# Campos de webhook da Meta — o que existe, o que assinamos, o que modelamos

*[Read in English](META-CAMPOS-DE-WEBHOOK.md)*

**Código:** `internal/meta/parse.go` (onde o formato da Meta vira o vocabulário do gateway),
`internal/meta/types.go` (o vocabulário), `internal/inbound/handler.go` (as guardas 5a/5b que
decidem se o evento chega ao consumidor), `testdata/corpus/` (os payloads congelados).

**O que este doc é:** o inventário dos campos de webhook do App, com a forma de cada payload.
**O que ele NÃO é:** captura de tráfego. Salvo indicação em contrário, cada exemplo abaixo é o
**sample do painel da Meta** (botão *Test* de cada campo, que mostra o corpo **sem enviar nada**),
coletado em **2026-07-28**. Vale a distinção da tabela de origem de `testdata/corpus/README.md`:
**sample de painel é "derivado da doc"** — a forma é oficial, os valores são fictícios, e nenhum
deles foi observado em tráfego real desta conta.

**Por que este doc existe:** em 2026-07-28, ao salvar a Callback URL nova, o App foi inscrito em
**dez** campos de uma vez, sem ninguém escolher. O `ONBOARDING-META.md` dizia "assine o campo
`messages`", como se fosse ação manual e única. Descobrir o que estava ligado — e o que **não**
estava — mudou duas decisões técnicas no mesmo dia (ver T-057 e T-058).

---

## Como ler a coluna "chega ao consumidor?"

O gateway modela poucos campos. **Campo não modelado não é descartado**: ele passa as guardas, é
entregue **sem evento nenhum**, e o consumidor grava o `cru`. Isso é comportamento correto, não falha
de parse — está no contrato, e todo consumidor precisa saber, senão trata evento vazio como erro.

> **No fio o campo vem como `"eventos": []`** — array vazio, sempre presente. A normalização é feita
> na montagem do envelope (`internal/inbound/deliver.go`, o `if evs == nil` na montagem do envelope) e travada por teste
> sobre os bytes (`TestEnvelopeWithNoEventGoesOutAsEmptyArrayOnTheWireNeverNull`).
>
> ⚠️ **De 2026-07-23 a 2026-07-28 ele era `null` (T-067), e este doc dizia `[]` — doc errado, não
> detalhe cosmético.** `ParseWebhook` devolve slice **nil** quando não enriquece nada
> (`internal/meta/parse.go`, no fim de `ParseWebhook`) e `json.Marshal` de slice nil produz `null`; do lado do consumidor,
> `for ev in envelope["eventos"]` estourava em Python e `if eventos == []` nunca casava. Ver
> `docs/CONTRATO-CONSUMIDOR.md`, seção *Webhook de CONTA* e *Mudanças que quebram*.

Duas guardas decidem se o evento chega (`internal/inbound/handler.go`, os blocos comentados como *guarda 5a* e *guarda 5b*):

- **5a — `phone_number_id`**: vale para payloads com `metadata.phone_number_id` (`messages`, `calls`).
- **5b — `waba_id`**: vale para webhooks de **conta**, que trazem o id da WABA em `entry[].id`.

*Consequência medida em 2026-07-28: o botão **Test** do painel envia um payload assinado
corretamente, mas com ids de exemplo (`waba_id: "0"`). Ele passa a assinatura e **morre na guarda
5b**, com `conta_descartada`. Ou seja: **o Test do painel não serve para descobrir o formato através
do gateway** — serve para provar que a guarda funciona. Para ver a forma, use o sample que o próprio
botão exibe, que é o que está neste doc.*

---

## Inscritos

> ⚠️ **A lista abaixo NÃO tem um número total, e a omissão é deliberada.** Ela foi levantada em
> 2026-07-28 com **dez** campos inscritos; no fim daquele mesmo dia o dono inscreveu
> `template_category_update` no painel (informado por escrito, **não medido daqui** — ver a nota no
> fim desta seção). Qualquer contagem escrita aqui é uma foto de um painel que alguém pode mexer sem
> avisar este arquivo. **Só uma coisa responde "quantos e quais estão inscritos AGORA":**
>
> ```
> GET https://graph.facebook.com/v25.0/{app-id}/subscriptions?access_token={app-id}|{app-secret}
> ```
>
> É leitura e não muda nada. O `app-id` é `869733115682937`.
>
> 🔴 **E ela não é fácil de rodar daqui, o que é parte do achado (T-057, 2026-07-28).** O `app_secret`
> **não está** em `/etc/zapgw/env` no CT 125 — aquele arquivo tem `ZAPGW_CHAVE_CIFRA`, `ZAPGW_BANCO`
> e `ZAPGW_ENDERECO`. Ele mora **cifrado no banco, por instância**, e nenhum comando do CLI o decifra
> de volta — de propósito (T-052: dos quatro segredos da instância, só `verify_token` e
> `segredo_entrega` são impressos, porque precisam existir fora do gateway). Quem for medir precisa do
> valor de outra fonte; **medido em 2026-07-28 que ele não está no ambiente do serviço.**

| Campo | Modelado? | Chega ao consumidor |
|---|---|---|
| `messages` | ✅ sim | eventos de mensagem e de status |
| `message_template_status_update` | ✅ sim (T-043) | `tipo: "template_status"` |
| `template_category_update` | ✅ sim (T-057) | `tipo: "template_categoria"` — **inscrito pelo dono em 2026-07-28**, depois do levantamento acima |
| `phone_number_quality_update` | ✅ sim (T-058) | `tipo: "qualidade_do_numero"` |
| `account_alerts` | ✅ sim (T-058) | `tipo: "alerta_de_conta"` |
| `account_update` | ❌ não | `"eventos": []` |
| `account_review_update` | ❌ não | `"eventos": []` |
| `security` | ❌ não | `"eventos": []` |
| `phone_number_name_update` | ❌ não | `"eventos": []` |
| `message_template_quality_update` | ❌ não | `"eventos": []` |
| `calls` | ❌ não | `eventos: []` — **mas é entregue**, ver abaixo |

> **Os cinco `❌` não são fila, são decisão — e a decisão tem prazo indeterminado de propósito**
> (T-058, 2026-07-28). O envelope só **CRESCE**: acrescentar um `tipo` depois é de graça, tirar
> depois é quebra de contrato. Um campo modelado sem consumidor interessado vira **vocabulário
> morto** que o gateway passa a dever para sempre, e ninguém nunca lê. **A ordem é: primeiro
> aparece quem consome, depois nasce o `tipo`.** Isso está escrito no contrato para o consumidor
> saber que pode pedir.
>
> A ordem de modelagem desta rodada foi **por custo**, não por completude:
> `template_category_update` (dinheiro imediato e janela de recurso), `phone_number_quality_update`
> (cota — falha de envio que aponta para o lugar errado), `account_alerts` (severidade). Os cinco
> restantes não têm nenhum dos dois. **O `calls` tinha uma pergunta de dono na frente; ela foi
> respondida e o assunto está ENCERRADO — ver a seção dele abaixo.** Resumo de uma linha: nenhum
> número do gateway aceita ligação, habilitar exige limite ≥ 2000 e os dois estão em `TIER_250`,
> então o evento **não tem como chegar**.

## Desinscritos que importam

| Campo | Por que importa |
|---|---|
| `message_template_components_update` | avisa mudança de componentes, que quebra a suposição de `indice` dos botões. **Aprovado pelo dono em 2026-07-28** ("sim, vamos tratar"), junto com o `template_category_update`; a inscrição dele **não foi confirmada** aqui |

---

## `template_category_update` — INSCRITO em 2026-07-28, e é o que a T-043 deveria ouvir

```json
{"message_template_id": 12345678, "message_template_name": "my_message_template",
 "message_template_language": "en-US",
 "previous_category": "MARKETING", "new_category": "UTILITY",
 "correct_category": "MARKETING", "category_appeal_status": "ELIGIBLE"}
```

A T-043 avisa da reclassificação `UTILITY` → `MARKETING` lendo a categoria de
`message_template_status_update`, que é o evento de **aprovação/rejeição** com a categoria como
atributo. **Este é o evento dedicado à mudança**, e ele dá o que o outro não dá: a **direção**
(`previous_` → `new_`) e o **`category_appeal_status`** — a reclassificação é **contestável**, e sem
receber o evento ninguém sabe que existe janela de recurso. Reclassificação para `MARKETING`
encarece cada envio.

**Fechado na T-057 (2026-07-28), em dois lados que precisavam dos dois:**

- **fora do repositório** — o dono inscreveu o campo no painel da Meta, campo a campo, com o
  alternador que **não toca** na Callback URL. Isto é afirmação do dono, **não medição desta
  sessão**: a consulta `GET /{app-id}/subscriptions` não foi feita, pela razão escrita na nota da
  seção *Inscritos*, acima;
- **dentro** — `internal/meta/parse.go` (`templateCategoryEvent`) e `internal/meta/types.go`
  (`TemplateCategory`) modelam o `value`, e o evento sai como `tipo: "template_categoria"`. O
  sample acima ficou congelado em `testdata/corpus/categoria_de_template_derivado_da_doc.json`,
  **marcado como derivado da doc no próprio nome do arquivo**, até a captura real aparecer.

✅ **Apareceu, e o fixture derivado saiu (T-174, 2026-08-28).** O consumidor `consumer-b` cedeu três
payloads crus pelo canal, congelados em `testdata/corpus/categoria_de_template_rebaixamento.json`,
`..._restauracao.json` e `..._sem_anterior.json`. **Dois achados que o sample do painel não daria:**
o par vai e volta no **mesmo** `message_template_id` (`UTILITY → MARKETING` e, ~14,9 h depois, a
volta), e **um dos três chegou sem `previous_category`** — que era o caso tratado por decisão de
projeto, sem observação nenhuma. ⚠️ **E nenhuma das três trouxe `correct_category` ou
`category_appeal_status`**, os dois campos que o parágrafo acima descreve a partir da documentação;
são três eventos de uma conta, então isso é medição, não conclusão sobre o comportamento da Meta.

**Consequência de as duas metades terem chegado no mesmo dia:** a partir de 2026-07-28 este webhook
chega de verdade em produção, então o modelo tem tráfego real para validá-lo. Enquanto o binário com
o modelo não subir, ele chega como webhook de conta não modelado — `cru` entregue, `"eventos": []`,
que é comportamento correto e documentado.

## `calls` — inscrito, NÃO é webhook de conta, e É entregue

```json
{"messaging_product": "whatsapp",
 "metadata": {"display_phone_number": "16505551111", "phone_number_id": "123456123"},
 "calls": [{"id": "ABGGFlA5Fpa", "to": "18005551180", "from": "16315551181",
            "timestamp": 1504902988, "event": "connect"}],
 "contacts": [{"profile": {"name": "test user name"}, "wa_id": "16315551181"}]}
```

Tem `metadata.phone_number_id`, então passa pela guarda **5a** — e numa ligação real o id **bate**,
logo o evento **chega ao consumidor** (sem evento nenhum, `"eventos": []`, até ser modelado). Duas coisas que nenhum
outro campo desta lista tem:

- **carrega dado pessoal** (`contacts[].profile.name`, `wa_id`) numa linha crua que ninguém lê —
  entra na conta da retenção;
- **`calls` é uma LISTA e uma ligação gera vários eventos** (`connect` e outros). Quem responder a
  cada evento manda várias mensagens para o mesmo cliente. O gatilho tem de ser **um** evento
  escolhido, com os demais ignorados explicitamente.

## 🛑 ENCERRADO em 2026-07-30 — NÃO vamos modelar `calls`, e o motivo é que ele não pode acontecer

**Decisão do dono, 2026-07-30:** a tarefa de modelar `calls` (T-076) foi **fechada como concluída**,
sem código. Não é adiamento por prioridade: é que **o evento não tem como chegar**.

**Os três fatos medidos que fecham o assunto:**

1. **Nenhum dos números do gateway aceita ligação.** Medido pelo dono no aparelho, 2026-07-30:
   *"nem o número da Padaria e nem o número da Lojinha tem como fazer ligação para eles."*
   Inscrição **não** gera evento — quem gera é a ligação. Com chamada desabilitada, o webhook `calls`
   **nunca dispara**.
2. **Não dá para habilitar nem "só para documentar".** Habilitar é uma chamada só e é reversível
   (`POST /<PHONE_NUMBER_ID>/settings` com `{"calling":{"status":"ENABLED"}}`; `DISABLED` desfaz),
   sem revisão e sem aprovação — **mas exige limite de mensagens ≥ 2000**:
   > *"Calling is not enabled by default on a business phone number. To enable calling, you must have
   > a messaging limit of 2000 or above."*
   Fonte: <https://developers.facebook.com/documentation/business-messaging/whatsapp/calling/call-settings>,
   lida em 2026-07-30.
3. **Os dois números estão em `TIER_250`** — medido em produção em 2026-07-31 02:22 UTC
   (= 2026-07-30 23:22 -03), `zapgw estado --slug …`, `numero_na_meta.limite_de_mensagens`,
   `estado: observado`, `fonte: medicao`. `tenant-two` e `tenant-one`, ambos `TIER_250` e ambos
   `GREEN` em qualidade. **Um quarto do mínimo exigido.**

*E mesmo qualificando não seria teste rápido: a mesma página avisa que* **"WhatsApp users may take up
to 7 days to reflect those changes"** *— o botão de ligar demora a aparecer para quem vai ligar.*

**O que NÃO muda com o encerramento:** o campo **continua inscrito** (não custa nada e não há motivo
para mexer), e a decisão do dono de 2026-07-28 — *"call a gente pode assinar e passar a tratar
respondendo com mensagens"* — **não foi revertida**. Ela ficou **inalcançável**, que é diferente:
no dia em que uma ligação puder chegar, é ela que vale como ponto de partida.

**O que reabre o assunto — precisa das DUAS:** (a) o número chegar a limite ≥ 2000, o que passa pela
verificação do negócio (**T-003**); e (b) a decisão de produto de **atender** ligação, porque
habilitar significa que alguém — pessoa ou automação — passa a precisar responder.

**O que já está resolvido e não deve ser refeito no dia D:**

- ✅ **A pergunta que bloqueava o desenho está RESPONDIDA na fonte:** uma ligação recebida **abre** a
  janela de 24 h e a **renova** a cada nova ligação —
  > *"When a WhatsApp user messages you or calls you, a 24-hour timer called a customer service
  > window starts. If the user messages or calls you again before the timer expires, the timer resets
  > to 24 hours."*
  (<https://developers.facebook.com/documentation/business-messaging/whatsapp/messages/send-messages>,
  lida em 2026-07-30.) **Então responder por mensagem não exige template.**
- 🔴 **Mas responder NÃO é grátis**, e a tarefa afirmava que era: a mesma página diz que *"Service
  messages are billed under the SERVICE pricing category"*. **Janela aberta dispensa TEMPLATE, não
  COBRANÇA.** O preço em si não foi conferido e não deve ser escrito sem conferir na página de
  pricing.
- ⚠️ **O exemplo de payload acima NÃO foi confirmado contra tráfego real.** Ele só mostra
  `event: "connect"`, e uma ligação gera vários eventos cujos nomes não estão listados em página
  nenhuma que foi aberta. **No dia D, o primeiro payload real vale mais que este exemplo**, e é sobre
  ele que a chave de dedup se desenha.

*Por que este encerramento está escrito aqui e não some junto com a tarefa: um item que apenas
desaparece é ressuscitado pela próxima leitura do histórico — foi o que aconteceu com o
`consultorio`, duas vezes. Encerramento que não é escrito não encerra nada.*

**NÃO foi modelado na T-058, e a razão está registrada porque a tarefa e este doc divergiam.** A
T-058 dizia *"antes de modelar, a pergunta é do dono: o número aceita ligação, e existe alguém para
atender? **Não modele antes dessa resposta**"* — e esta seção já trazia a resposta, dada no mesmo
dia: sim, e a resposta é mensagem. **A tarefa estava desatualizada em relação ao doc, não o
contrário.** Ainda assim ele ficou de fora, por dois motivos que sobrevivem à correção:

1. **a pergunta que decide o DESENHO continua aberta** (a janela de 24 h). Modelar o evento sem ela
   é modelar metade: o gatilho existiria e ninguém saberia se a resposta pode ser texto livre;
2. **ele não é webhook de conta e o trabalho é de outra natureza** — guarda 5a em vez de 5b, uma
   LISTA de eventos por ligação (responder a cada um manda várias mensagens ao mesmo cliente), e
   dado pessoal no `cru`. É tarefa própria, não um item de uma rodada de webhooks de conta.

*Enquanto isso ele continua chegando ao consumidor com `"eventos": []` e com `contacts[].profile.name`
e `wa_id` dentro do `cru` — e isso já entra na conta de retenção dos dois consumidores, hoje. Está
dito no contrato, na tabela de webhooks de conta.*

## `phone_number_quality_update` — inscrito e MODELADO (T-058), é a COTA do número

```json
{"display_phone_number": "16505551111", "event": "ONBOARDING",
 "current_limit": "TIER_250", "old_limit": "TIER_NOT_SET",
 "max_daily_conversations_per_business": "TIER_250"}
```

`current_limit`/`old_limit` dão o teto diário **e a direção**. Rebaixamento de tier ou número
sinalizado chegam por aqui. Sem modelar, um rebaixamento aparece só quando o envio começa a falhar
por limite — sintoma que aponta para o lugar errado. **Os valores são preservados literais**
(`"TIER_250"`, não convertido para número: a Meta pode criar tier novo).

**Modelado na T-058** como `tipo: "qualidade_do_numero"` — ver `NumberQuality`
(`internal/meta/types.go`), `numberQualityEvent` (`internal/meta/parse.go`) e a seção do
evento em `docs/CONTRATO-CONSUMIDOR.md`.

**E desde a T-080 (2026-07-28) ele é a SEGUNDA FONTE de um bloco do estado.** O `current_limit` é
gravado em `numero_na_meta.limite_de_mensagens` com `fonte: "webhook"`
(`internal/inbound/handler.go`, `recordNumberLimit`); a outra fonte é a medição da vigia
(`internal/outbound/vigia.go`). Quem desempata é `config.UpdateNumberAtMeta`
(`internal/config/numero.go`), pela regra **"vence a observação mais recente, qualquer que seja a
fonte"** — e o carimbo comparado é o **nosso** relógio, não o `entry.time`, porque comparar dois
relógios não sincronizados decidiria o desempate em silêncio.

> ⚠️ **`current_limit` ≠ `max_daily_conversations_per_business`.** O estado grava o **primeiro**, que
> é a mesma grandeza que a medição lê em `whatsapp_business_manager_messaging_limit`. É exatamente a
> troca que o fixture sintético desta seção existe para acusar.

> ⚠️ **Neste sample, `current_limit` e `max_daily_conversations_per_business` têm o MESMO valor**
> (`"TIER_250"`). Congelá-lo como único fixture produziria um corpus em que trocar a leitura de um
> campo pelo outro passa **verde** — medido. Por isso o corpus tem também
> `qualidade_do_numero_sintetico.json`, com os três limites diferentes e um **rebaixamento**
> (`TIER_1K → TIER_50`), que é a direção que o sample não exercita. Ver
> `testdata/corpus/README.md`.

## `message_template_quality_update` — inscrito

```json
{"previous_quality_score": "GREEN", "new_quality_score": "YELLOW",
 "message_template_id": 12345678, "message_template_name": "my_message_template",
 "message_template_language": "pt-BR"}
```

Queda de qualidade de um template precede ele ser pausado pela Meta. Tem direção
(`previous_` → `new_`), como o de categoria.

## `message_template_components_update` — DESINSCRITO

```json
{"message_template_id": 12345678, "message_template_name": "my_message_template",
 "message_template_language": "en-US",
 "message_template_title": "message header", "message_template_element": "message body",
 "message_template_footer": "message footer",
 "message_template_buttons": [{"message_template_button_type": "URL",
                               "message_template_button_text": "button text",
                               "message_template_button_url": "https://example.com",
                               "message_template_button_phone_number": "12342342345"}]}
```

Traz a **lista de botões** do template. É exatamente o que avisaria que a posição dos botões mudou —
e o `indice` que o consumidor manda em `botoes_template` é a posição **no template**. Hoje o
mapeamento "bate por convenção, não por construção" (registrado pelos dois consumidores em
2026-07-26); este evento é o que transformaria convenção em aviso.

## `account_alerts` — inscrito e MODELADO (T-058)

```json
{"entity_type": "WABA", "entity_id": 123456, "alert_severity": "INFORMATIONAL",
 "alert_status": "NONE", "alert_type": "OBA_APPROVED",
 "alert_description": "Sample alert description, informational in nature with no status"}
```

`alert_severity` existir implica severidades acima de `INFORMATIONAL` — e são essas que importam.

**Modelado na T-058** como `tipo: "alerta_de_conta"` — ver `AccountAlert`
(`internal/meta/types.go`) e `accountAlertEvent` (`internal/meta/parse.go`). `alert_severity`,
`alert_type` e `alert_status` são repassados **como vieram**: o gateway não ordena severidades e não
deriva "grave" de nada, porque ordenar vocabulário de terceiro exige conhecer a lista inteira e
ninguém aqui a conhece.

**Este sample NÃO ganhou irmão sintético**, e a decisão é o oposto da do `phone_number_quality_update`
logo acima, pela mesma pergunta: os campos que entram na chave (`entity_id`, `alert_type`,
`alert_severity`, `alert_status`) já vêm com valores **diferentes entre si** aqui, então ele sozinho
pega leitura de campo trocada. Um sintético "por simetria" seria cerimônia sem garantia.

⚠️ **`entity_id` vem como NÚMERO** (`123456`, sem aspas) e sai do gateway como **texto** — mesma
tolerância do `message_template_id`, que na captura real tem 16 dígitos e não cabe em `int32`.

## `account_update` — inscrito

```json
{"phone_number": "16505551111", "event": "VERIFIED_ACCOUNT"}
```

## `history` — DESINSCRITO, e provavelmente não se aplica aqui

Entrega **conversas antigas em pedaços** (`metadata.phase`, `chunk_order`, `progress`), com
`threads[].messages[]` e `history_context.from_me`. É a importação de histórico de quem migra **do
app WhatsApp Business** para a Cloud API. A migração do `consumer-b` veio da Evolution, não do app,
então não se aplica — fica registrado para o dia em que alguém migrar um número que tenha histórico
no aparelho.

---

## Campos que existem e nunca foram olhados

Do painel, em 2026-07-28, todos **desinscritos**: `account_settings_update`, `automatic_events`,
`business_capability_update`, `business_status_update`, `business_username_updates`, `flows`,
`group_lifecycle_update`, `group_participants_update`, `group_settings_update`,
`group_status_update`, `message_echoes`, `messaging_handovers`, `partner_solutions`,
`payment_configuration_update`, `smb_app_state_sync`, `smb_message_echoes`, `standby`,
`template_correct_category_detection`, `tracking_events`, `user_preferences`.

**Não os inscreva por curiosidade.** Campo inscrito e não modelado vira linha crua no banco de um
consumidor em produção — volume e dado guardado sem uso. Inscreva quando houver quem consuma.
