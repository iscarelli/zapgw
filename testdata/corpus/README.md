# Corpus de payloads da Meta

Payloads **reais**, com números, ids e nomes trocados por valores de teste. O **formato** é
preservado byte a byte — é ele que está sendo testado, não o conteúdo.

**Nunca ponha dado pessoal aqui.** Ao acrescentar um payload novo: troque `wa_id`, `from`,
`recipient_id`, `user_id`/`from_user_id`/`recipient_user_id`, `id` (wamid) e `profile.name` por
valores fictícios, e confira que nenhum texto de mensagem real sobrou.

> 🔴 **O `wamid` é obrigatório na lista acima, e não por precaução: ele CARREGA o telefone do
> destinatário.** `wamid.` é seguido de base64, e `base64 -d` no que vem depois do ponto devolve o
> número em texto claro. Trocar `recipient_id` e deixar o `wamid` vaza o número do mesmo jeito, e o
> arquivo parece mascarado. **Campo opaco não é campo sem conteúdo — decodifique antes de deixar
> passar.** (Medido ao mascarar a captura da T-069; ver a nota própria no fim deste arquivo.)

Cada arquivo carrega, no nome, o que ele prova. Um arquivo sem teste que o consuma não deve existir:
`corpus_test.go` falha se algum `.json` daqui não for exercitado.

## Origem de cada arquivo

A marcação é **por arquivo**, não por lote — um lote misto (a forma antiga deste README) vira
"todos são reais" na cabeça de quem lê rápido. Todo arquivo abaixo é um destes três:

- **captura**: veio de tráfego real da Meta, observado por um consumidor em produção
  (`consumer-a`), com valores sensíveis mascarados de propósito (coordenadas arredondadas,
  `id`/`sha256`/`url` de mídia substituídos por placeholder). A **forma** é literal, byte a byte;
  os **valores** que apontariam para uma pessoa real, não.
- **derivado da doc**: não havia captura real disponível; a forma foi copiada dos exemplos
  publicados pela documentação oficial da Meta, com os ids trocados para o padrão fictício deste
  corpus (`WABA_TESTE`, `PNID_TESTE`, `wamid.TESTEnnn`).
- **sintético**: não é payload que a Meta mandou nem a doc descreveu — foi escrito à mão para
  exercitar um caminho que nem captura nem doc cobrem sozinhos (ex.: `corpo_null.json` prova a
  guarda de `json.Unmarshal("null", &map)`; `botao_de_template_sintetico.json` existe porque a
  captura real tem `payload == text` e não pegaria sozinha uma leitura de campo trocada).

## Lastro por STATUS — os quatro têm captura real desde a T-069 (2026-07-28)

**Leia isto antes de usar o corpus para validar um mapeamento de status.** A coluna "Origem" da
tabela abaixo é por ARQUIVO; ela responde *"de onde veio este arquivo?"* e **não** responde a
pergunta que o integrador realmente faz, que é *"contra o que eu estou testando o meu `sent`?"*.
Esta seção responde essa.

| Status | Arquivo no corpus | Lastro |
|---|---|---|
| `sent` | `status_sent_com_pricing.json` e `status_sent_sem_pricing.json` | ✅ **captura** — consumer-a, 2026-07-28 (T-069). São **dois** arquivos porque a medição achou **duas formas**: 49 dos 53 `sent` crus com `pricing`, **4 sem** |
| `delivered` | `status_delivered.json` | ✅ **captura** — consumer-a, 2026-07-28 (T-069). 49 `delivered` crus, **49 com `pricing`** |
| `read` | `status_read_com_cobranca.json` | ✅ captura real (parcial) — consumer-a, 2026-07-26 |
| `failed` | `status_failed.json` | ✅ captura real — consumer-a, 2026-07-26 |

**Até 2026-07-28 esta seção dizia o oposto, e o que ela dizia vale guardar como aviso**, porque a
mesma armadilha volta a cada tipo novo: `sent` não tinha fixture nenhum (`grep -rln '"sent"'` no
corpus voltava **vazio**) e `delivered` tinha um **derivado da doc**. Quem testasse contra aquele
`delivered` provava que concorda com a **documentação da Meta**, não com o que a Meta **manda** — é
a família *"exemplo de doc é código que ninguém roda"* (`docs/ARMADILHAS.md`) um nível acima: ali o
exemplo virou **fixture**, e fixture verde **parece prova**.

**A captura confirmou que a desconfiança valia a pena: o `delivered` derivado da doc estava errado
em dois pontos observáveis.** Ele não trazia `pricing` (e o real traz, 49 de 49) e trazia um bloco
`conversation` que **nenhum dos três payloads reais capturados tem**. Nenhum dos dois muda
comportamento — `conversation` nunca foi lido pelo parser, e a ausência de `pricing` era tratada —,
e é justamente por isso que o caso é instrutivo: um fixture pode estar errado sobre a forma sem que
nenhum teste fique vermelho.

**Por que o gap de `sent` sobreviveu tanto tempo sem ninguém notar — e a lição estrutural CONTINUA
VALENDO, porque nada mudou no mecanismo:** `TestCorpusInteiro` (`internal/meta/corpus_test.go`)
varre os arquivos que **existem** e exige que cada um tenha teste. Nada exige o contrário — **que
todo status tenha arquivo**. A guarda protege contra arquivo órfão e é cega para status ausente. Se
a Meta criar um estado novo amanhã, ele some do corpus exatamente como `sent` sumiu.

**Os quatro nomes não são um vocabulário fechado no código.** O parser repassa o `status` como veio
(`internal/meta/parse.go:eventoDeStatus`, e o valor entra na chave do evento na mesma função); a
lista `sent, delivered, read, failed` existe **só como comentário** em `internal/meta/types.go`. Ou
seja: não há, hoje, nenhum lugar do código que pudesse ficar vermelho por causa de um status novo ou
de um status sem fixture.

| Arquivo | Origem |
|---|---|
| `mensagem_texto.json` | **captura** — consumer-a, 2026-07-26 (T-031). Confirma `from_user_id` e `contacts[].user_id` (formato `BR.<dígitos>`) no tráfego real — campos que NÃO aparecem nos exemplos clássicos da doc |
| `botao_de_template.json` | **captura** — consumer-a, 2026-07-26 (T-031). Quick-reply de template real, tocado no aparelho: `type: "button"`, e `payload`/`text` vieram **iguais** (`"Falar com a gente"`) — ver `botao_de_template_sintetico.json` abaixo |
| `botao_de_template_sintetico.json` | **sintético** (T-031). Mesmo formato do capturado acima, mas com `payload` e `text` DIFERENTES de propósito — é o que pega leitura de campo trocado, que o capturado (valores iguais) não pega sozinho |
| `botao_interativo.json` | **captura** — consumer-a, 2026-07-26 (T-033). `button_reply.id` (`"confirmar"`) e `button_reply.title` (`"Confirmar"`) vêm DIFERENTES — o suficiente para distinguir sozinho uma leitura de campo trocada; **não tem irmão sintético** (ver nota abaixo) |
| `reacao.json` | **captura** — consumer-a, 2026-07-26 (T-026). Reação real com emoji `❤️` (`U+2764 U+FE0F`, dois codepoints — ver a nota abaixo) |
| `reacao_removida.json` | **captura** — consumer-a, 2026-07-26 (T-026). É o par observado da linha acima: mesma reação (mesmo alvo), desfeita 20s depois. A chave `emoji` não existe no payload — não é `""`, não é `null` |
| `localizacao.json` | **captura** — consumer-a, 2026-07-26 (T-026). Pin solto: **sem** `name`/`address` — o caso comum observado, ao contrário do fixture anterior (derivado da doc), que tinha os dois e testava o caso raro |
| `audio_nota_de_voz.json` | **captura** — consumer-a, 2026-07-26 (T-026). `"voice": true` e `mime_type: "audio/ogg; codecs=opus"` (com o parâmetro) confirmados no payload real |
| `imagem.json` | **captura** — consumer-a, 2026-07-26 (T-026) |
| `video.json` | **captura** — consumer-a, 2026-07-26 (T-026) |
| `documento_com_legenda.json` | **captura** — consumer-a, 2026-07-26 (T-030). `caption` e `filename` vêm os dois, lado a lado, e o `filename` é o nome real (longo, com hífens e números) do arquivo do cliente |
| `mensagem_encaminhada_sintetica.json` | **sintético** (T-059, 2026-07-28). Não existe captura real destes campos — `grep -rl forwarded testdata/corpus/` não achava nada antes deste arquivo. `context.forwarded` e `context.frequently_forwarded` vêm com valores DIFERENTES entre si (`true`/`false`), pela mesma razão de `botao_de_template_sintetico.json` existir; e o `context` **não tem `id`**, porque encaminhar não é citar |
| `resposta_a_mensagem.json` | **captura** — consumer-a, 2026-07-26 (T-032). Mensagem de texto respondendo (citando) outra: traz `context.id` com o `wamid` da mensagem citada, ao lado de `context.from` (o número do NEGÓCIO — não modelado, ver `types.go`) |
| `status_sent_sem_pricing.json` | **captura** — consumer-a, 2026-07-28 (T-069). O `sent` **sem** o bloco `pricing`: 4 dos 53 `sent` crus medidos (~7,5%). É o arquivo que prova que `pricing` é opcional; ver a nota própria abaixo |
| `status_sent_com_pricing.json` | **captura** — consumer-a, 2026-07-28 (T-069). A forma comum do `sent` (49 dos 53), com `pricing` `{"billable":false,...,"category":"service"}`. Mesmo `wamid` e mesmo `timestamp` do `status_delivered.json` — de propósito, ver a nota própria abaixo |
| `status_delivered.json` | **captura** — consumer-a, 2026-07-28 (T-069). **Substituiu** o fixture derivado da doc que existia aqui (não convive com ele). Vem com `pricing` (49 de 49 no corpus deles) e **sem** o bloco `conversation` que o derivado tinha |
| `status_failed.json` | **captura** — consumer-a, 2026-07-26 (T-033). É a falha real de 2026-07-20 (OS LR-00014, `code 131026`, `"Message undeliverable"`) que motivou o aviso ao operador que deu origem à T-028; antes era derivado do exemplo genérico da doc (`code 131049`) |
| `status_read_com_cobranca.json` | **captura (parcial)** — consumer-a, 2026-07-26 (T-041), colada no canal bilateral (`consumer-a-STATUS.local.md`, gitignored). O trecho `{"status":"read","pricing":{"billable":true,"pricing_model":"PMP","category":"utility","type":"regular"}}` é literal, exatamente como eles colaram; envolvido aqui no envelope-padrão deste corpus (`WABA_TESTE`/`PNID_TESTE`/`wamid.TESTE017`) porque o que foi colado não incluía `entry`/`changes`/`metadata` |
| `status_de_template.json` | **captura (parcial)** — consumer-a, 2026-07-26 (T-043). O `change` inteiro (`field` + `value`) é literal: um dos 21 exemplares que eles guardavam em disco desde antes da migração. O nível `entry` (`id`/`time`) é o envelope-padrão deste corpus, porque o que foi entregue não incluía esse nível — e o `time` **é lido** pelo parser (entra na chave do evento), então ele não é enfeite: ver a nota própria abaixo |
| `context_de_tipo_errado_sintetico.json` | **sintético** (T-061, 2026-07-28). `context` vem como **string** onde se espera objeto. Ninguém observou a Meta fazer isso; o arquivo descreve o que o gateway tem de AGUENTAR, não o que ela manda — ver a nota própria abaixo |
| `context_com_campo_de_tipo_errado_sintetico.json` | **sintético** (T-061, 2026-07-28). O `context` é objeto, mas `id` vem **número** onde se espera string e `forwarded` vem **string** onde se espera booleano. É o caso que o arquivo acima não cobre: lá o parser nem entra no bloco |
| `audio_voice_de_tipo_errado_sintetico.json` | **sintético** (T-061, 2026-07-28). Áudio com `voice` **string** onde se espera booleano — o gêmeo mais velho do `context` no mesmo defeito (o formato frágil de `voice` vinha do plano 1) |
| `texto_de_tipo_errado_sintetico.json` | **sintético** (T-062, 2026-07-28). `"text":"oi"` — string onde se espera objeto, no tipo de mensagem mais comum de todos. Traz uma **irmã sã** no mesmo lote; ver a nota própria abaixo |
| `audio_de_tipo_errado_sintetico.json` | **sintético** (T-062, 2026-07-28). O **bloco de mídia inteiro** com tipo errado (`"audio":"MEDIA_TESTE10"`), um nível acima do `voice` que a T-061 fechou. Irmã sã no mesmo lote |
| `interativo_de_tipo_errado_sintetico.json` | **sintético** (T-062, 2026-07-28). `"interactive":"button_reply"` — string onde se espera objeto. Irmã sã no mesmo lote |
| `reacao_de_tipo_errado_sintetico.json` | **sintético** (T-062, 2026-07-28). `"reaction":"wamid.TESTE001"` — string onde se espera objeto. É o arquivo que separa **bloco ausente** de **bloco ilegível**: a guarda "reação sem alvo é malformada" continua valendo, e não alcança este caso. Irmã sã no mesmo lote |
| `botao_de_tipo_errado_sintetico.json` | **sintético** (T-062, 2026-07-28). `"button":"Falar com a gente"` — string onde se espera objeto, na resposta a botão de template (o caminho que funciona fora da janela de 24h). Irmã sã no mesmo lote |
| `metadata_de_tipo_errado_sintetico.json` | **sintético** (T-068, 2026-07-28). `"metadata":"PNID_TESTE"` — string onde se espera objeto, um nível ACIMA da mensagem. Traz mensagem **e** status no mesmo `change`, porque era o `change` inteiro que morria |
| `contacts_de_tipo_errado_sintetico.json` | **sintético** (T-068, 2026-07-28). `"contacts":"Fulana de Teste"` — o mais caro dos cinco medidos: apagava o **lote inteiro de mensagens de um cliente**. Mensagem e status no mesmo `change` |
| `field_de_tipo_errado_sintetico.json` | **sintético** (T-068, 2026-07-28). `"field":42` no primeiro `change`, com um segundo `change` são. É o **único dos seis que continua devolvendo `ErrParseParcial`** — ver a nota própria abaixo |
| `entry_id_de_tipo_errado_sintetico.json` | **sintético** (T-068, 2026-07-28). `"id":42` no primeiro `entry` (o `waba_id`), com um SEGUNDO `entry` são — que é como a Meta batcha contas diferentes na mesma chamada |
| `status_de_tipo_errado_sintetico.json` | **sintético** (T-068, 2026-07-28). `status`, `recipient_id` e `timestamp` de tipo inesperado no primeiro status, com um status irmão são. O `timestamp` numérico **sobrevive** (é a exceção tolerante); os outros dois degradam para vazio |
| `template_de_tipo_errado_sintetico.json` | **sintético** (T-068, 2026-07-28). `message_template_name` número e `reason` objeto, com um segundo `change` de template são. O `event` e a `message_template_category` — o que faz o evento valer a pena — sobrevivem |
| `categoria_de_template_rebaixamento.json` | **captura** (T-174, 2026-08-28, cedida pelo consumidor `consumer-b`). `template_category_update` real: `UTILITY → MARKETING` no `instrucoes_download_app_v6`, `entry.time` 1787252135 (2026-08-20 18:55:35 UTC = 15:55:35 -03). Corpo inteiro, sem reformatar; a única troca na origem foi o `waba_id` por `WABA_TESTE`. Ver a nota própria abaixo |
| `categoria_de_template_restauracao.json` | **captura** (T-174, 2026-08-28, mesma origem). A **volta** do MESMO `message_template_id`: `MARKETING → UTILITY`, `entry.time` 1787305767 (2026-08-21 09:49:27 UTC = 06:49:27 -03). Só o par prova que ida e volta saem com chaves de dedup diferentes |
| `categoria_de_template_sem_anterior.json` | **captura** (T-174, 2026-08-28, mesma origem). Chega **sem `previous_category`** (`teste_sonda_503_20ago`, `new_category: MARKETING`, `entry.time` 1787244576). Uma em 18 eventos guardados pelo consumidor; é a primeira evidência real do caso que `parse.go` tratava por decisão de projeto |
| `categoria_de_template_sintetico.json` | **sintético** (T-057, 2026-07-28). Mesmo formato, com `previous_category`, `new_category` e `correct_category` DIFERENTES entre si — no sample do painel `previous` e `correct` vêm **iguais** (`MARKETING`), e por isso ele sozinho não pega leitura de campo trocada. Mesma razão de `botao_de_template_sintetico.json` existir. **Sobreviveu à chegada das capturas (T-174) e não por hábito:** nenhuma das três traz `correct_category` ou `category_appeal_status`, então ele é o único arquivo que ainda exercita esses dois campos |
| `qualidade_do_numero_derivado_da_doc.json` | **derivado da doc** (T-058, 2026-07-28). Sample do botão *Test* do painel para `phone_number_quality_update`, congelado byte a byte. O `display_phone_number` (`16505551111`) é o número **fictício da própria Meta**, preservado do sample — não é o número de ninguém, e não há telefone real neste arquivo |
| `qualidade_do_numero_sintetico.json` | **sintético** (T-058, 2026-07-28). Os TRÊS limites diferentes entre si (no sample `current_limit` == `max_daily_conversations_per_business`), e a direção **cara**: `TIER_1K → TIER_50`, um rebaixamento. Ver a nota própria abaixo |
| `alerta_de_conta_derivado_da_doc.json` | **derivado da doc** (T-058, 2026-07-28). Sample do botão *Test* do painel para `account_alerts`. **Não tem irmão sintético**, e a ausência é decisão: os campos que entram na chave já vêm com valores diferentes entre si no sample, então ele sozinho pega leitura de campo trocada (mesma decisão de `botao_interativo.json`) |
| `corpo_null.json` | sintético (não é payload da Meta — prova a guarda de `json.Unmarshal("null", &map)`) |

## Sobre os arquivos DERIVADOS da documentação — **restam dois, e o nome deles diz isso**

> 🔴 **Esta seção dizia "não existe mais nenhum" até 2026-07-28.** A T-057 e a T-058 acrescentaram
> `categoria_de_template_derivado_da_doc.json`, `qualidade_do_numero_derivado_da_doc.json` e
> `alerta_de_conta_derivado_da_doc.json`, e os três eram derivados **por falta de alternativa** — são
> webhooks de CONTA cujo tráfego é raro por natureza (uma reclassificação de categoria, um
> rebaixamento de tier e um alerta de conta não acontecem toda semana), e o
> `template_category_update` estava até **desinscrito** no App. Nenhum consumidor tinha um exemplar
> guardado.
>
> ✅ **Um dos três já saiu: a T-174 (2026-08-28) apagou `categoria_de_template_derivado_da_doc.json`**
> e pôs três capturas reais no lugar (`..._rebaixamento`, `..._restauracao`, `..._sem_anterior`),
> cedidas pelo consumidor `consumer-b`. **Ele não sobrevive ao lado delas** — é a regra logo abaixo,
> aplicada. Continuam derivados `qualidade_do_numero_derivado_da_doc.json` e
> `alerta_de_conta_derivado_da_doc.json`.
>
> **O nome do arquivo carrega a marcação.** A tabela de origem é o registro, mas quem abre o
> diretório vê o arquivo antes de abrir este README, e um fixture derivado que se parece com um
> capturado é exatamente o que esta seção existe para não deixar acontecer de novo.
>
> **A regra de substituição vale para os dois que restam:** quando aparecer captura real, ela
> **substitui** o arquivo — os dois não convivem.


Para a T-023 (reação, localização, legenda/nome de arquivo) e a T-028 (motivo de falha no status)
não havia payload real disponível quando essas tarefas foram feitas — nenhum consumidor tinha
exercitado esses caminhos ainda contra a Meta de verdade. Esses arquivos **não eram captura real**:
a forma foi copiada dos exemplos publicados pela documentação oficial da Meta, com os ids trocados
para o mesmo padrão fictício do resto do corpus, e nome/endereço de localização (quando presentes
num exemplo) inventados.

Eles foram sendo substituídos por captura à medida que o tráfego real apareceu:
`botao_interativo.json` e `status_failed.json` na T-033 (2026-07-26), **`status_delivered.json`,
o último daquela safra, na T-069 (2026-07-28)**, e `categoria_de_template_derivado_da_doc.json` na
T-174 (2026-08-28). Contando hoje, **dois arquivos da tabela acima são derivados da doc**
(`qualidade_do_numero_derivado_da_doc.json` e `alerta_de_conta_derivado_da_doc.json` — T-058);
todos os outros são *captura* ou *sintético*.

> **A justificativa que segurou o `delivered` derivado era conforto, não argumento — e a captura
> deu razão a quem desconfiou.** Ela dizia que não haveria o que confirmar num `delivered` real,
> porque *"`delivered` sem motivo é exatamente o caso feliz"*. A frase raciocina sobre o campo
> `errors[]`, que de fato não apareceria — e conclui, sem base, sobre o **payload inteiro**. A T-051
> (2026-07-28) já a tinha marcado como frágil; a T-069, horas depois, mostrou que o derivado errava
> a forma em dois pontos (`pricing` ausente, `conversation` presente). **"Não há o que confirmar" só
> se sabe depois de capturar** — e os achados deste corpus são todos de coisas que ninguém tinha
> como prever lendo a doc: `from_user_id`/`user_id` em `mensagem_texto.json`, `reason: "NONE"` e a
> ausência de `metadata` em `status_de_template.json`, a chave `emoji` **ausente** em
> `reacao_removida.json`, e agora o `pricing` opcional do `sent`.

Se um arquivo derivado voltar a nascer aqui (um tipo novo sem captura), a regra continua: quando o
real aparecer, **substitua** o arquivo e atualize a tabela acima — não deixe os dois convivendo.

## Sobre os arquivos de CAPTURA real (T-026, 2026-07-26)

O `consumer-a` capturou uma bateria de mensagens reais mandadas pelo dono do número de teste:
texto, emoji, localização, imagem, áudio e vídeo — e, numa captura separada no mesmo dia, uma
reação e sua remoção 20 segundos depois. Os valores sensíveis foram mascarados **por eles**, antes
de colar no canal bilateral (`consumer-a-STATUS.local.md`, gitignored): coordenadas arredondadas,
`id`/`sha256`/`url` de mídia substituídos por placeholder — porque a localização real aponta para a
casa de uma pessoa, e as URLs `lookaside.fbsbx.com` são credenciais temporárias de acesso ao
arquivo. A forma (quais chaves existem, em que nível, com que tipo) é literal.

**`reacao_removida.json` é o arquivo mais importante do lote**: é o único caso em que o significado
inteiro do evento está na AUSÊNCIA de uma chave, e é o único fato desta tarefa que não podia ser
confirmado por leitura de doc — só por alguém ver a reação sumir e capturar o payload.

## Sobre `documento_com_legenda.json` (T-030, 2026-07-26)

Era o último fixture de mensagem-com-mídia ainda marcado "derivado da doc" — nenhum consumidor
tinha mandado documento com legenda até então. O `consumer-a` capturou um do fio e colou no canal
bilateral (`consumer-a-STATUS.local.md`, gitignored), com os mesmos mascaramentos do lote da T-026:
`sha256`/`id`/`url` substituídos por placeholder (as URLs `lookaside.fbsbx.com` são credenciais
temporárias de acesso ao arquivo). A forma — quais chaves existem, em que nível, com que tipo — é
literal; `caption` e `filename` são o texto real observado.

A captura confirma a mesma estrutura que a doc já previa (`caption` e `filename` lado a lado), então
não é um achado de formato — mas ela substitui um valor de teste fraco por um realista: o `filename`
derivado da doc era `recibo-teste.pdf` (curto, sem hífen, sem número); o capturado é
`515642-9741-manual-forno-gourmet-grill-rev-43.pdf`, o nome real do arquivo do cliente. Um fixture
com nome curto nunca teria exercitado nome longo, com hífens e dígitos.

**Esta frase foi corrigida em 2026-07-26 (T-031) depois de sair errada uma vez.** Uma versão anterior
deste parágrafo dizia "nenhum fixture de mensagem descreve a doc" como se fosse absoluto — o
qualificador real, dito pelo consumer-a, era "o último derivado **que importa**", e a troca por um
absoluto foi feita sem contar arquivo nenhum. Contando na época (T-031): dos fixtures de MENSAGEM, só
`botao_interativo.json` continuava "derivado da doc" (`mensagem_texto.json` e `botao_de_template.json`
tinham virado captura na mesma tarefa); `status_failed.json` ficava de fora da contagem por ser
fixture de STATUS, não de mensagem. **A T-033 (2026-07-26) fechou os dois que restavam** — ver a
nota própria mais abaixo — então, desde ali, nenhum fixture de MENSAGEM do corpus é derivado da doc;
`status_delivered.json` (fixture de STATUS) foi o último de todos, e caiu na T-069 (2026-07-28).

## Sobre `mensagem_texto.json` e `botao_de_template.json` (T-031, 2026-07-26)

O consumer-a capturou dois fixtures que ainda estavam "derivado da doc" e os dois trouxeram algo
que o derivado não tinha:

**`mensagem_texto.json`** — o payload real chegou com `contacts[].user_id` e
`messages[].from_user_id`, formato `BR.<dígitos>`, identidade de usuário. Nenhum exemplo clássico da
doc da Meta mostra esses campos, e ninguém sabe desde quando eles existem no tráfego real. Isso não
quebra nada hoje — `encoding/json` ignora campo desconhecido em silêncio — mas até esta tarefa o
corpus **nunca tinha exercitado** esse caminho: um corpus que só contém campos já conhecidos não pode
falhar por campo novo, que é exatamente o cenário para o qual ele existe. A garantia agora está
provada por teste (`TestParseWebhookCampoDesconhecidoNaoDerrubaOParse`,
`internal/meta/parse_test.go`), não só pelo acidente do `encoding/json`.

**Decisão explícita: `user_id`/`from_user_id` NÃO entram no envelope** (`Evento`, em `types.go`). É
dado de identidade de pessoa, nenhum consumidor pediu, e o envelope só cresce — acrescentar depois é
de graça, tirar depois é quebra de contrato. O mesmo teste que prova que o campo não derruba o parse
também prova que ele não vaza para o envelope.

**`botao_de_template.json`** — o quick-reply de template real trouxe `payload` e `text` **iguais**
(`"Falar com a gente"`). Um fixture assim passa verde mesmo se o parser ler o campo errado (os dois
valores coincidem) — é a mesma família do "teste de vazamento cuja fixture apagava o ramo que
vazaria" (`docs/ARMADILHAS.md`). Por isso `botao_de_template_sintetico.json` existe: mesmo formato,
`payload` e `text` DIFERENTES de propósito. Provado por mutação: trocar
`e.BotaoPayload = m.Button.Payload` por `m.Button.Text` em `internal/meta/parse.go` deixa
`TestParseWebhookBotaoDeTemplateSinteticoDistinguePayloadDeTexto` vermelho e o teste do capturado
(`TestParseWebhookBotaoDeTemplateCapturaTemPayloadIgualATexto`) continua verde — exatamente porque os
valores do capturado são iguais.

**Dois não-achados, verificados antes de escrever a tarefa (não supostos):** o parser não lê
`context` (presente no payload real do botão, mas não modelado em `mensagemMeta`), então a
mensagem original apontada por `context.id`/`context.from` não afeta nada aqui; e `type: "button"`
já era tratado como caminho próprio antes desta tarefa (`internal/meta/parse.go`), com o comentário
já existente dizendo que quick-reply de template não chega como `interactive.button_reply`. Nenhum
dos dois virou achado porque nenhum dos dois divergiu do esperado.

**Esta nota ficou desatualizada em 2026-07-26 (T-032): o parser passou a ler `context`.** O
`consumer-a` pediu o campo — é o último que faltava para eles removerem a lib privada que existia
só por causa do envelope incompleto. `botao_de_template.json` (acima) já carregava `context` desde a
T-031 e nunca tinha sido lido; agora é: `context.id` vira `Evento.ResponderA` (`responder_a` no
JSON), com o MESMO nome do campo equivalente no envio (`Pedido.ResponderA`) — razão na T-024.
`context.from` continua de fora, e agora por decisão explícita, não por acidente: é o número do
NEGÓCIO, não do cliente, e um campo que parece "de quem" e é "para quem" é convite a bug.

## Sobre `resposta_a_mensagem.json` (T-032, 2026-07-26)

> **A ausência de `responder_a` é o caso NORMAL — inclusive em resposta de verdade.** Observado em
> 2026-07-26 (consumer-a, dois payloads da mesma conversa, 3 min de diferença): responder **segurando
> a bolha** gera `context`; responder **digitando na notificação** (inline) gera payload **sem
> `context`**. A Meta não manda vínculo nesse caso. Como responder pela notificação é o caminho mais
> rápido no celular, a ausência é provavelmente a maioria do tráfego.
>
> **Este fixture cobre só o caso COM citação.** Não há captura do caso inline aqui — quem for
> escrever teste de ausência hoje usa payload sintético e deve marcá-lo como tal. Se aparecer captura
> do inline, ela entra e esta nota vira ponteiro para o arquivo.

Captura real do consumer-a: o dono respondeu citando uma mensagem anterior (segurando a bolha e
digitando "Recebido"). O `cru` trouxe `context.from` (o número do negócio) e `context.id` (o `wamid`
da mensagem citada) dentro de `messages[0]`. Os dois valores são DIFERENTES — o que faz este fixture
ser também a prova da mutação obrigatória da T-032: se o parser lesse `context.from` em vez de
`context.id`, `Evento.ResponderA` sairia com o número do negócio (`5532999990000`) em vez do `wamid`
esperado (`wamid.TESTE001`), e o teste que compara o valor exato (não só a presença do campo) fica
vermelho. A mesma distinção já existia, sem ter sido aproveitada, em `botao_de_template.json`
(`context.from` = `5532999990000`, `context.id` = `wamid.TESTE013`).

## Sobre `botao_interativo.json` e `status_failed.json` (T-033, 2026-07-26)

Os dois últimos fixtures de mensagem/status ainda marcados "derivado da doc" caíram na mesma
tarefa: o consumer-a auditou os 218 payloads crus que tem gravados (contando as chaves que a Meta
manda contra as que o nosso código lê — a mesma disciplina de "conte, não estime" desta rede) e
achou captura real para os dois.

**`botao_interativo.json`** — o consumer-a tinha dito, num ciclo anterior, que esse tráfego não
existia (mensagem interativa não-template nunca tinha sido mandada). A contagem desmentiu: existem
**três**. O `button_reply` capturado tem `id` (`"confirmar"`) DIFERENTE de `title` (`"Confirmar"`) —
os dois só diferem em capitalização, mas isso já basta para uma comparação de string em Go (que é
sensível a caixa) distinguir sozinha uma leitura de campo trocada. **Por isso este fixture NÃO ganhou
um irmão sintético** como `botao_de_template_sintetico.json` ganhou: lá o capturado tinha `payload ==
text` byte a byte, e por isso não pegava a mutação sozinho; aqui pega. Acrescentar um sintético "por
simetria" com o de template seria cerimônia sem garantia — a mesma pergunta de
`docs/ARMADILHAS.md`: "este campo/arquivo compra alguma diferença de comportamento, ou só parece que
sim porque outro parecido precisou?". A captura também trouxe `context` (não testado por este
fixture — `resposta_a_mensagem.json`, acima, já é quem prova a leitura de `context.id`).

**`status_failed.json`** — é a falha real de 2026-07-20 (OS LR-00014, `code 131026`, `"Message
undeliverable"`) que motivou o aviso ao operador que deu origem à T-028 inteira; antes era derivado
do exemplo genérico da doc oficial (`code 131049`, um código diferente do incidente que a tarefa
existia para resolver). A captura confirma campo a campo o que a T-028/T-029 já modelavam: `code`,
`title`, `message` e `error_data.details` existem, e **`title` e `message` vêm com o MESMO valor**
(`"Message undeliverable"`) — o corte de guardar só `codigo`/`mensagem` (não os dois) continua
certo, e `detalhes` (`"Message Undeliverable."`) continua sendo a única parte que acrescenta
informação além do título.

**O que esta tarefa acrescentou ao PARSER, não só ao corpus:** a Meta também manda `errors[]`
DENTRO de `messages[]` (não só em `statuses[]`), no sub_tipo `"unsupported"` — a Meta recebeu algo
que a Cloud API não sabe representar (`code 131051`, `"Message type unknown"`, formato confirmado em
developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/messages/unsupported/,
lido em 2026-07-26). Antes da T-033, `mensagemMeta` não tinha campo `Errors`, então uma mensagem
`unsupported` chegava com `sub_tipo` e um id, e nada mais — indistinguível de "mensagem vazia" para
o consumidor. O gateway reusa o MESMO `ErroStatus` e o MESMO campo `Evento.Erro` que o evento de
status já usa (ver `internal/meta/types.go`), porque o formato do item de `errors[]` é idêntico nos
dois lugares — só o SIGNIFICADO muda com o `tipo` do evento, e essa diferença está documentada no
código e em `docs/CONTRATO-CONSUMIDOR.md`. Não há fixture de corpus para esse caso (nenhuma captura
real chegou ainda); os testes que cobrem `sub_tipo: "unsupported"` em
`internal/meta/parse_test.go` usam um payload sintético, com os valores do exemplo oficial da Meta
citado acima.

## Sobre `mensagem_encaminhada_sintetica.json` (T-059, 2026-07-28)

É o único fixture de mensagem do corpus que **não tem lastro nenhum em tráfego observado** — nem
captura, nem exemplo de doc. Os dois lados foram procurados antes de escrevê-lo: nenhum dos payloads
guardados pelos consumidores traz `forwarded` (`grep -rl forwarded testdata/corpus/` volta vazio sem
este arquivo), e a documentação pública da Meta, procurada em 2026-07-28, **não tem mais página
descrevendo os campos de `context`** — as páginas de referência de webhook que existem hoje não os
mencionam. Ele descreve, portanto, o formato que o gateway **decidiu ler**, não um formato observado.

**Consequência a levar a sério, e é o motivo de esta nota existir:** um consumidor que teste o
mapeamento de encaminhamento só contra este arquivo prova que concorda com **a nossa suposição**, não
com o que a Meta manda. É a mesma família da T-051 (`sent` sem captura, `delivered` derivado da doc),
um grau pior. Quando aparecer captura real de mensagem encaminhada — os dois consumidores estão em
produção recebendo, e mensagem encaminhada é comum —, ela **substitui** este arquivo, não convive com
ele.

**Os dois campos vêm com valores diferentes entre si de propósito** (`forwarded: true`,
`frequently_forwarded: false`): com os dois iguais, trocar a leitura de um pelo outro passaria verde.
Provado por mutação (T-059): trocar `m.Context.Forwarded` por `m.Context.FrequentlyForwarded` em
`internal/meta/parse.go` deixa este fixture VERMELHO nas duas asserções ao mesmo tempo, enquanto
`TestParseWebhookCorrenteMarcaOsDoisCamposDeEncaminhamento` (payload sintético com os dois `true`)
continua verde — exatamente a assimetria que justifica os dois testes existirem.

O `context` deste arquivo **não tem `id`**: encaminhar não é citar, e os dois casos são
independentes na Meta. É por isso que o teste do corpus também exige `responder_a` ausente aqui.

## Sobre os três arquivos de TIPO ERRADO (T-061, 2026-07-28)

São os únicos arquivos do corpus **malformados de propósito** — e a distinção importa ao lê-los: os
outros descrevem o que a Meta *manda*; estes descrevem o que o gateway *tem de aguentar*. Nenhum dos
dois consumidores observou a Meta mandar tipo errado nestes campos, e nada aqui afirma que ela mande.

**O que eles provam é uma linha só, e ela estava invertida até 2026-07-28:** `err == nil` e
`len(evs) == 1`. Antes da T-061, um `context` (ou um `voice`) com tipo inesperado derrubava o
`json.Unmarshal` da **mensagem inteira** — ela virava `ignorados++` e sumia da lista `eventos`, sem
alarme e sem contador, com `200` respondido à Meta (que por isso nunca reenvia). Ou seja: um teste
verde aqui não é "o parser leu um campo esquisito", é **"a mensagem da cliente chegou"**.

Os três estão separados de propósito, um caminho por arquivo: bloco inteiro ilegível
(`context` string), campo ilegível **dentro** de um bloco legível (`id` número, `forwarded` string) e
o gêmeo em outro nível da árvore (`voice` string, dentro de mídia). Juntar dois num arquivo só faria
um teste vermelho não dizer qual caminho quebrou.

**Mutação obrigatória (T-061), feita e revertida antes do commit:** voltar `mensagemMeta.Context`
para struct plana deixa os dois primeiros vermelhos; voltar `midiaMeta.Voice` para `*bool` deixa o
terceiro. Detalhes das quatro mutações — incluindo as duas que provam decisões que nenhum payload
mal formado pegaria sozinho — em `docs/ARMADILHAS.md`, seção *Go / JSON*.

## Sobre os cinco arquivos de TIPO ERRADO por TIPO DE MENSAGEM (T-062, 2026-07-28)

São a continuação direta dos três acima, e a diferença entre os dois lotes é a lição da tarefa: os
da T-061 cobrem **dois campos**; estes cobrem **uma classe**. Medido com `ParseWebhook` antes da
T-062, um valor de tipo inesperado em **qualquer** campo de `mensagemMeta` — não só
nos cinco que dão nome a estes arquivos — fazia a mensagem virar `ignorados++` e **sumir de
`eventos`**. `"text":"oi"` é o caso que assusta: é o tipo mais comum de todos.

**Cada arquivo tem DUAS mensagens, e a segunda é o que o lote da T-061 não tinha: a irmã sã.** A
asserção que vale nos cinco é `len(evs) == 2` — a quebrada degrada e chega, a irmã chega intacta.
Um arquivo por tipo (e não um lote só com as cinco) porque teste vermelho tem de dizer **qual** tipo
quebrou.

**O que estes arquivos NÃO provam, e é de propósito:** eles cobrem cinco campos, não a struct
inteira. A
garantia de classe não está aqui — está em dois testes de `internal/meta/parse_test.go`:
`TestParseWebhookNenhumCampoDeTipoErradoApagaAMensagemNemAsIrmas` (varre **as chaves do próprio
payload**, tipo por tipo, com dois mutantes cada) e
`TestMensagemMetaIsolaTodoCampoPorConstrucao` (percorre a struct por reflexão e fica vermelho no dia
em que alguém pendurar ali um campo que não seja `json.RawMessage`). Fixture cobre o caso que
alguém pensou; esses dois cobrem o que ninguém escreveu ainda.

**Uma exceção continua existindo e está escrita:** `id` de tipo inesperado **continua** apagando a
mensagem (`TestParseWebhookIdDeTipoErradoContinuaApagandoAMensagem`). Sem wamid não há chave de
dedup, e `42` não vira o wamid `"42"` — inventar wamid mandaria o consumidor responder a uma
mensagem que não existe.

## Sobre os seis arquivos de TIPO ERRADO nos NÍVEIS ACIMA da mensagem (T-068, 2026-07-28)

Terceiro lote da mesma família, e a diferença dele para os dois anteriores é o **raio**. Os da T-061
e da T-062 estão todos dentro de `messages[]`: o que se perdia era uma mensagem. Estes estão nos
níveis que **contêm** a mensagem, e ali o que se perdia era o lote — medido com `ParseWebhook` antes
da tarefa, um campo trocado por vez, com mensagem boa + irmã boa no mesmo lote:

| Arquivo | Campo trocado | Antes da T-068 |
|---|---|---|
| `metadata_de_tipo_errado_sintetico.json` | `value.metadata` | `len(evs) = 0` — o `change` inteiro, mensagens **e** status |
| `contacts_de_tipo_errado_sintetico.json` | `value.contacts` | `len(evs) = 0` — idem |
| `field_de_tipo_errado_sintetico.json` | `change.field` | `len(evs) = 0` — o `change` inteiro |
| `entry_id_de_tipo_errado_sintetico.json` | `entry.id` (`waba_id`) | o `entry` inteiro sumia (irmãs de OUTROS `entry` sobreviviam) |
| `status_de_tipo_errado_sintetico.json` | `status.status`/`recipient_id`/`timestamp` | o status sumia (a irmã sobrevivia) |
| `template_de_tipo_errado_sintetico.json` | `template.message_template_name`/`reason` | `len(evs) = 0` — o evento de template |

**`contacts` é o pior, e é pior que o defeito que a T-062 acabou de consertar:** um `"contacts":"x"`
que a Meta mandasse num formato novo apagava o lote inteiro de mensagens de um cliente, calado, com
`200` respondido à Meta — que é o Critical nº 1 de `docs/ARMADILHAS.md` com outro nome.

**Dois destes arquivos afirmam mais do que "não sumiu", e é por isso que eles existem separados:**

- **`field_de_tipo_errado_sintetico.json` é o único que continua devolvendo `ErrParseParcial`**, de
  propósito. `field` é o campo pelo qual o `change` é **classificado** — sem ele não dá para saber se
  aquele `value` era um webhook de template que deixamos de modelar. As mensagens chegam (melhor
  esforço, o `change` é lido como se fosse `messages`), e o `parse_error` do envelope diz que algo não
  pôde ser classificado. A regra geral, herdada da T-062: **conta-se `ignorados` quando um EVENTO
  deixa de existir, nunca quando um bloco dentro de um evento entregue se perde** — por isso
  `metadata` e `contacts` ilegíveis NÃO contam.
- **`entry_id_de_tipo_errado_sintetico.json` documenta uma decisão que não é do parser**, e sim da
  guarda de isolamento: `entry.id` ilegível vira `""`, e a guarda 5b de `internal/inbound/handler.go`
  trata `""` como **não-casado**, recusando o lote com `ALARME` e `conta_descartada`. A alternativa
  (descartar só o `entry` e entregar o resto) foi recusada porque o corpo **cru** vai junto na
  entrega: filtrar eventos não impediria o conteúdo daquela conta de chegar ao consumidor. Provado
  por `TestHandlerRecusaWebhookDeContaComWabaIDIlegivel` (`internal/inbound/handler_test.go`).

**A garantia de CLASSE, como no lote da T-062, não está nestes arquivos** — está em
`TestStructsDeFronteiraIsolamTodoCampoPorConstrucao` (reflexão sobre as sete structs de fronteira) e
em `TestParseWebhookNenhumCampoDeNenhumNivelCalaOLote`, que varre **todas as chaves de todos os
níveis do próprio payload** e exige que a mensagem-testemunha de outro `entry` sempre chegue.

## Sobre `status_read_com_cobranca.json` (T-041, 2026-07-26)

O consumer-a pediu (2026-07-26) o campo `pricing` que a Meta manda no webhook de status — presente
em 145 dos 148 status que eles têm gravados — porque a categoria de cobrança que ele traz é o único
jeito de saber, na PRIMEIRA mensagem, que a Meta reclassificou um template (`UTILITY` →
`MARKETING`, o que muda preço e regras de envio); sem ele, isso só apareceria na fatura, semanas
depois. O trecho que eles colaram no canal bilateral era só o par `status`/`pricing`, sem envelope —
diferente dos outros fixtures de captura deste corpus, que vieram do payload inteiro. Este arquivo
embrulha esse trecho literal no mesmo envelope-padrão (`WABA_TESTE`/`PNID_TESTE`) dos outros
fixtures de status, porque `TestCorpusInteiro` (`internal/meta/corpus_test.go`) exercita
`ParseWebhook` sobre o payload inteiro, não sobre um fragmento solto.

**Por que o campo se chama `cobranca` no envelope, não `pricing`:** o formato da Meta morre no
`parse.go`, como em todo o resto do envelope — é o mesmo motivo de `reacao`/`localizacao` não se
chamarem `reaction`/`location`. Só `categoria` (de `category`) e `cobravel` (de `billable`) são
modelados; `pricing_model` e `type` ficam de fora até alguém precisar deles.

**`cobravel` é ponteiro, não `bool`** — mesma razão de `voz` (`Evento.Voz`), com consequência maior:
aqui a diferença entre "a Meta disse que não cobra" (`false`) e "a Meta não disse nada" (ausente) é
de DINHEIRO. Provado por mutação: trocar `Cobranca.Cobravel` de `*bool` para `bool` não deixa só um
teste vermelho — quebra a COMPILAÇÃO de `internal/meta/corpus_test.go` e `internal/meta/parse_test.go`
(`invalid operation: ... == nil (mismatched types bool and untyped nil)`), porque os testes exigem
comparar o campo contra `nil` para provar que ausência e `false` produzem resultados diferentes.

## Sobre `status_de_template.json` (T-043, 2026-07-26)

É o primeiro fixture do corpus que **não é sobre uma mensagem nem sobre um destinatário**: é um
webhook de CONTA (`field: "message_template_status_update"`), a Meta avisando que um template foi
aprovado, rejeitado ou pausado. Até a T-043 esse payload chegava, o `ParseWebhook` não achava
`messages` nem `statuses`, e o envelope saía sem evento nenhum e só com o `cru` — naquela época
literalmente `"eventos": null` no fio, não `[]`, que foi o defeito consertado na T-067 (2026-07-28).

**Dois fatos do payload real não eram dedutíveis da doc, e os dois estão neste arquivo de
propósito:**

- **`reason` vem como a string `"NONE"`** quando não há motivo — não ausente, não `null`. Quem
  tratar como campo opcional erra. O gateway repassa `"NONE"` como veio; só a ausência REAL some do
  JSON (`internal/meta/types.go`, `StatusDeTemplate.Motivo`).
- **não há `metadata` nem `phone_number_id` em lugar nenhum do payload** — confirma com dado a
  lacuna que a T-038 fechou por leitura de código: a única chave de roteamento de um webhook de
  conta é o `waba_id` de `entry[].id`.

**O `message_template_id` e o `message_template_name` NÃO foram trocados por valores fictícios**, ao
contrário de `wa_id`/`from`/`recipient_id`/`wamid`/`profile.name`, que a regra do topo deste arquivo
manda mascarar. Não é descuido: nenhum dos dois é dado pessoal (um é o id de um template no painel
da Meta, o outro é o nome que o próprio negócio deu a ele), e o id literal tem **16 dígitos** — trocá-lo
por um id curto de teste deixaria de exercitar o caminho que importa (ele não cabe em `int32`, e é
por isso que `templateStatusMeta.TemplateID` é `json.RawMessage` e vira texto, nunca inteiro).

**O `entry.time` deste arquivo (`1769000020`) é do envelope-padrão, não da captura — e mesmo assim é
lido.** O `value` deste webhook **não tem carimbo próprio**; o único tempo disponível está no
`entry`, e ele entra na CHAVE do evento (`template_status:{id}:{event}:{time}`) porque o mesmo
template pode ser `APPROVED` mais de uma vez (aprovado → editado → pendente → aprovado de novo).
Sem o tempo na chave, a segunda aprovação seria deduplicada pelo consumidor e sumiria. Ver
`internal/meta/parse.go`, `eventoDeStatusDeTemplate`, e o teste que prova isso
(`TestParseWebhookStatusDeTemplateDuasAprovacoesEmInstantesDiferentesTemIdsDiferentes`).

## Sobre os três arquivos de `sent`/`delivered` REAIS (T-069, 2026-07-28)

`status_sent_com_pricing.json`, `status_sent_sem_pricing.json` e `status_delivered.json`.

**A amostra, porque é ela que justifica os fixtures existirem.** O `consumer-a` mediu o corpus
inteiro deles — **267 payloads guardados**, dos quais **225 são o corpo cru da Meta** (os 42
restantes são o envelope do gateway, que carrega o cru em base64 dentro de si). Dentro dos 225:

| Medição | Número |
|---|---|
| `sent` | **53** — **49 com `pricing`**, **4 sem** (~7,5%) |
| `delivered` | **49** — **49 com `pricing`** (100%) |
| `recipient_user_id` presente | **152** dos 225 |
| `contacts[].user_id` presente | **203** dos 225 |

Não são três exemplos escolhidos a dedo: são três payloads de uma medição sobre o corpus inteiro, e
**a proporção é o que diz quais formas precisam existir aqui**. "4 em 53" é a razão de haver DOIS
fixtures de `sent`; um só teria congelado a forma comum e deixado o caso normal de fora.

### O que estes três mostram, e nenhum fixture escrito à mão mostraria

1. **`pricing` é OPCIONAL no `sent`.** Um `sent` sem cobrança não é payload quebrado, é rotina em
   ~7,5% do tráfego medido. Quem for contar por categoria de cobrança precisa saber disso **antes**
   de escrever o contador. Provado por mutação: fazer o parse exigir `pricing` deixa
   `status_sent_sem_pricing.json` vermelho.
2. **`recipient_user_id` existe no status, formato `BR.<dígitos>`** — e `contacts[].user_id` junto.
   Nenhum dos dois é modelado por `statusMeta`/`contatoMeta`, e desde a T-062/T-068 isso é decisão e
   não acidente. Estes fixtures **provam** o que antes se supunha: chave desconhecida no status não
   derruba o parse. Provado por mutação: ligar `DisallowUnknownFields` no `Unmarshal` de `statusMeta`
   deixa os três vermelhos.
3. **O `sent` e o `delivered` do MESMO `wamid` vieram com o MESMO `timestamp` da Meta.** É o achado
   mais valioso, porque a consequência é do **consumidor**: quem ordenar histórico pelo relógio do
   emissor não separa os dois estados. Está preservado aqui de propósito — os dois arquivos
   compartilham `wamid.TESTE042` e `timestamp` `1785072102` — e trancado por
   `TestCorpusSentEDeliveredDoMesmoWamidTemOMesmoTimestamp`. O aviso ao consumidor está em
   `docs/CONTRATO-CONSUMIDOR.md`; um README que só nós lemos não protege quem monta o histórico.

### Como foram mascarados

As capturas vieram verbatim do `psql` do consumidor, **com número de cliente e `wamid` reais**. O
mascaramento é **consistente** — o mesmo valor real vira sempre o mesmo valor falso, nos três
arquivos —, porque sem isso o par `sent`→`delivered` deixaria de ser o mesmo envio e o teste de
correlação não provaria nada:

| Campo | Virou |
|---|---|
| `entry[].id` (WABA) | `WABA_TESTE` |
| `metadata.phone_number_id` | `PNID_TESTE` |
| `metadata.display_phone_number` | `5532999990000` (o número de negócio padrão deste corpus) |
| `contacts[].wa_id` e `statuses[].recipient_id` | `553288888888` |
| `contacts[].user_id` e `statuses[].recipient_user_id` | `BR.20000000000000000` |
| `statuses[].id` (o `wamid`) | `wamid.TESTE041` (o `sent` sem pricing) e `wamid.TESTE042` (o `sent` com pricing **e** o `delivered`) |

**O `wamid` real precisava sair inteiro, e não só "trocar os dígitos que parecem número".** O wamid
da Cloud API é `wamid.` seguido de **base64**, e esse base64 carrega o telefone do destinatário
**em texto claro lá dentro** — `base64 -d` no que vem depois do ponto devolve o número. Um
mascaramento que trocasse `recipient_id` e deixasse o `wamid` teria vazado o número do mesmo jeito,
e ninguém veria olhando o arquivo. **A regra que fica: campo opaco não é campo sem conteúdo — antes
de deixar um identificador passar por "não parece dado pessoal", decodifique-o.**

**Os `timestamp` NÃO foram mascarados, de propósito** (`1785073298` e `1785072102`): não identificam
ninguém, e o segundo é literalmente o fato que a captura provou. Trocá-los por valores do
envelope-padrão apagaria o achado nº 3.

**O bloco `conversation` não existe em nenhum dos três** — o fixture derivado da doc que este lote
substituiu tinha um. O parser nunca leu esse bloco (`statusMeta` não tem o campo), então a diferença
não muda comportamento; ela só mostra que um fixture pode errar a forma sem nenhum teste ficar
vermelho.

> ⚠️ **`553288888888` é um destinatário NOVO neste corpus — os outros fixtures usam outro número, e
> a divergência é deliberada.** Ao mascarar esta captura, decodificar o base64 do `wamid` real
> mostrou que o destinatário do tráfego real era **o mesmo número** que este corpus vinha tratando
> como fictício desde o plano 1: era o telefone pessoal do dono, não um valor inventado. **Resolvido
> pela T-159 (2026-08-20):** todo fixture que usava esse número passou a usar `5511999990000` (a
> convenção que `docs/CONTRATO-CONSUMIDOR.md` já usa), o mesmo padrão adotado horas antes pela T-138
> para o mesmo tipo de vazamento. A divergência com `553288888888` continua existindo — são dois
> valores sintéticos diferentes, um por captura de tráfego real mascarado, outro pela substituição da
> T-159 — mas nenhum dos dois é mais o telefone real. Unificá-los, se algum dia fizer sentido, é
> decisão fora do escopo da T-159.

## Sobre os quatro arquivos de `template_category_update` (T-057, 2026-07-28; T-174, 2026-08-28)

Três capturas — `categoria_de_template_rebaixamento.json`, `..._restauracao.json` e
`..._sem_anterior.json` — mais `categoria_de_template_sintetico.json`. O derivado da doc que abria
esta lista **foi apagado pela T-174**; a seção sobre os derivados, acima, explica por que ele não
convive com as capturas.

**Nenhum dos quatro contém telefone, `wa_id`, `wamid` ou nome de pessoa** — o `value` deste webhook
não tem nada disso. Foi o primeiro grupo do corpus em que a regra de mascaramento do topo deste
arquivo não teve o que mascarar, e vale dizer isso em vez de deixar o leitor conferir campo a campo.
Nas três capturas a única troca feita na origem foi o `waba_id` por `WABA_TESTE`; o
`message_template_id` ficou como veio, de propósito, porque é ele que prova que o par é do **mesmo**
template.

**Por que o sintético continua existindo depois das capturas — e a razão MUDOU.** Ele nasceu porque
o sample do painel trazia `previous_category: "MARKETING"` e `correct_category: "MARKETING"` — o
**mesmo valor** —, e um parser que lesse um no lugar do outro passaria **verde**. Hoje a razão é mais
forte: **nenhuma das três capturas traz `correct_category` ou `category_appeal_status`**, então o
sintético é o único arquivo do corpus que ainda exercita esses dois campos. É a mesma família de
`botao_de_template.json` (`payload == text` na captura real) e da entrada *"teste de vazamento cuja
fixture apagava o ramo que vazaria"* de `docs/ARMADILHAS.md`.

*Medido, não suposto (T-057): a mutação que troca `t.Anterior` por `t.Correta` em
`eventoDeCategoriaDeTemplate` (`internal/meta/parse.go`) deixava **só** o sintético vermelho — o
derivado da doc continuava verde, com o `ID` do evento idêntico. Feita e revertida antes do commit.*

**A direção CARA agora tem captura.** O sample do painel mostrava `MARKETING → UTILITY`, que
*barateia*, e por isso o sintético foi feito com `UTILITY → MARKETING`, que **encarece cada envio**.
A T-174 congelou as duas direções em tráfego real, do **mesmo** `message_template_id`, com ~14,9 h
entre uma e outra — o que o sample não podia mostrar de jeito nenhum.

**`category_appeal_status` só existe no sintético** (`NOT_ELIGIBLE`), e ele é repassado como
**texto**. Um booleano derivado dele ("dá para recorrer?") obrigaria o gateway a decidir hoje o que
fazer com um valor que a Meta só inventa amanhã. ⚠️ **Que ele não tenha vindo em nenhuma das três
capturas é medição, não conclusão:** são três eventos de uma conta, e a doc da Meta lista o campo. O
teste `TestCategoriaDeTemplateNenhumaCapturaRealTrouxeRecursoNemCategoriaCorreta` congela o que foi
observado e fica vermelho no dia em que uma captura com o campo entrar — que é o dia de atualizar a
medição e a tabela do contrato juntas, não de apagar a asserção.

**O `message_template_id` do sintético tem 16 dígitos** (`9900000000000002`), como o da captura de
`status_de_template.json`: ele não cabe em `int32`, e é por isso que `templateCategoriaMeta.TemplateID`
é `json.RawMessage` lido como texto, nunca inteiro. O do derivado é o `12345678` do sample, preservado
byte a byte — congelar o sample é o que o arquivo faz.

## Sobre os três arquivos de webhook de CONTA da T-058 (2026-07-28)

`qualidade_do_numero_derivado_da_doc.json`, `qualidade_do_numero_sintetico.json` e
`alerta_de_conta_derivado_da_doc.json`.

**Nenhum contém telefone de cliente, `wamid` ou nome de pessoa.** O `display_phone_number` do
derivado é `16505551111` — o número fictício que a própria Meta usa nos samples do painel, preservado
byte a byte; o do sintético é `5532999990000`, o número de negócio padrão deste corpus. A regra de
mascaramento do topo deste arquivo não tem o que mascarar aqui.

### Por que a qualidade tem irmão sintético e o alerta não

**É a mesma pergunta nos dois casos, e ela deu respostas diferentes** — o que é justamente o motivo
de a pergunta valer a pena: *dois campos vizinhos deste payload têm o mesmo valor?*

- **qualidade: sim.** O sample traz `current_limit: "TIER_250"` e
  `max_daily_conversations_per_business: "TIER_250"` — **o mesmo valor**. Congelar só ele produziria
  um corpus em que trocar a leitura de um pelo outro passa **VERDE**. *Medido: a mutação
  `q.LimiteAtual` → `q.LimiteDiarioMaximo` em `eventoDeQualidadeDoNumero`
  (`internal/meta/parse.go`) deixa vermelho **apenas** `qualidade_do_numero_sintetico.json`; o
  derivado da doc continua verde, com o `ID` do evento idêntico. Feita e revertida antes do commit.*
- **alerta: não.** `entity_type`, `entity_id`, `alert_severity`, `alert_status` e `alert_type` vêm
  todos com valores diferentes no sample, então ele sozinho distingue leitura trocada. Acrescentar um
  sintético "por simetria" seria cerimônia sem garantia — a mesma decisão, e a mesma pergunta, de
  `botao_interativo.json`.

**É a terceira vez que essa pergunta rende neste corpus** (`botao_de_template.json` com
`payload == text`; o `categoria_de_template_derivado_da_doc.json` com `previous == correct` — arquivo
apagado pela T-174, mas o achado é o que sobrevive dele; e agora `current_limit == max_daily`). Ela é
grátis e vale como passo fixo: **antes de congelar um payload, olhe se dois campos vizinhos têm o
mesmo valor.**

### O sintético também congela a direção que DÓI

O sample do painel mostra `TIER_NOT_SET → TIER_250` com `event: "ONBOARDING"` — a única transição de
cota que não preocupa ninguém. O sintético mostra `TIER_1K → TIER_50` com `event: "FLAGGED"`: um
**rebaixamento**, que é exatamente o caso que este evento existe para avisar antes de o envio começar
a falhar por limite. Um corpus que só tivesse o sample congelaria a boa notícia.

### Os limites são TEXTO, e os fixtures provam isso

`"TIER_250"` não vira `250`, `"TIER_NOT_SET"` não vira `0` nem vazio, e `TIER_10K` existe no sintético
para que ninguém sinta vontade de fazer aritmética com o sufixo. A Meta pode inventar um tier novo
amanhã, e uma tabela de tradução nossa erraria do pior jeito: devolvendo um número plausível para um
valor que ninguém verificou. Ver `QualidadeDoNumero`, em `internal/meta/types.go`.

### `entity_id` vem NÚMERO e sai TEXTO

O sample manda `"entity_id": 123456` (número JSON), e o gateway o repassa como a string `"123456"` —
mesma tolerância (e mesmo motivo) do `message_template_id`, que na captura real tem 16 dígitos e não
cabe em `int32`. O fixture derivado já exercita esse caminho, então não há sintético para isso.
