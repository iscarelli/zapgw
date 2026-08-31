# Onboarding de um número na Meta (WhatsApp Cloud API)

*[Read in English](ONBOARDING-META.md)*

**Código:** os valores coletados aqui viram uma linha da tabela `instancia` — os campos estão em
`internal/config/store.go` (`type Instancia`). Este doc descreve **como obtê-los**, não o schema.

> **Aviso de doutrina.** A Meta muda este processo com frequência, e este projeto proíbe afirmar como
> fato o que não foi conferido na fonte. As **etapas** e as **dependências** abaixo são estáveis e o
> que importa para o planejamento. Os **números específicos** (limites de tier, prazos exatos, se App
> Review é exigido) estão marcados *[CONFERIR na fonte]* — confirme em `developers.facebook.com/docs/whatsapp`
> e no Business Manager **no dia**, não por esta página.

## O que a verificação trava, e o que NÃO trava

**A verificação do negócio NÃO é portão para começar a enviar.** Existe um tier **não-verificado**:
um número registrado envia desde já, limitado a cerca de **250 conversas iniciadas pelo negócio por
24h** *[CONFERIR o número vigente e o que conta como "conversa"]*. A **Business Verification** (mais
nome de exibição aprovado e qualidade mantida) **destrava tiers maiores** (1k, 10k, 100k…) — é portão
para **escalar**, não para **lançar**.

Consequência para o planejamento — e é o oposto do que uma versão anterior deste doc dizia: **o
caminho crítico para a primeira mensagem NÃO é a Meta; somos nós.** Ao volume baixo com que o consumer-c
começa, 250/24h é folgado, então o que separa o gateway de funcionar é o **deploy** (plano 4) e o
**registro do número** — ambos nossos. A verificação corre em paralelo, como frente de "destravar
escala", e só vira gargalo quando o volume se aproximar do teto não-verificado.

Três pontos que decidem quão folgado é o teto de 250, e que este doc **não** afirma sem conferir:
- é só para conversa **iniciada pelo negócio** (template/marketing)? Responder a quem mandou mensagem
  primeiro, dentro da janela de atendimento, pode **não** contar — o que importaria muito para um
  gateway majoritariamente transacional. *[CONFERIR]*
- o **nome de exibição** precisa de aprovação para o número sair do estado inicial, ou só para exibir
  bonito? *[CONFERIR]*
- o número exato e a janela (24h corridas? por número? por WABA?). *[CONFERIR]*

## Quem preenche cada campo: você, ou o consumidor (T-079, 2026-07-28)

**Este documento descreve o onboarding de um número na conta Meta DE QUEM OPERA O GATEWAY.** Quando a
conta Meta é de um **terceiro** — o caso que o gateway passou a suportar na T-079 —, as fases abaixo
continuam valendo, mas quem as executa é **ele**, na conta dele, e quem digita os valores no gateway
também é ele:

| Caminho | Quem cria a instância | Quem preenche `waba_id`, `phone_number_id`, número, `app_secret`, `token_envio`, `callback_url` |
|---|---|---|
| conta Meta **do dono** (produção própria, laboratório) | dono, com `--waba-id …` e as demais flags | dono, no mesmo comando (ou por `zapgw instancia rotacionar`) |
| conta Meta **do consumidor** (terceiro) | dono, **só com `--slug`** | **o consumidor**, por `POST /v1/cadastro`, na janela de 24 h |

No segundo caminho o dono **não conhece, não guarda e não pede** esses valores — e o comando **não
sorteia** `app_secret` nem `token_envio` (sortear faria `zapgw instancia mostrar` dizer
`app_secret=sim` sobre um valor que a Meta do consumidor nunca viu; ver `docs/ARMADILHAS.md`). O que
o dono entrega está em `docs/CONTRATO-CONSUMIDOR.md`, *"O que você recebe ao ser provisionado"*.

**`verify_token` e `segredo_entrega` continuam sendo sorteados e impressos pelo gateway nos dois
caminhos** — eles são do gateway, não da Meta de ninguém. No caminho do terceiro, eles fazem parte do
pacote de entrega.

## O que cada campo da instância precisa, e de onde vem

| Campo (`Instancia`) | O que é | De onde vem no onboarding |
|---|---|---|
| `PhoneNumberID` | id do número na Graph API (**não** é o telefone) | aparece no painel do WhatsApp depois que o número é registrado |
| `WabaID` | id da WhatsApp Business Account | Business Settings → contas do WhatsApp |
| `DisplayNumber` | o telefone em si, como exibido | o número que você registra |
| `AppSecret` | segredo do App Meta; **verifica o HMAC** do webhook de entrada | App → Configurações → Básico → Chave Secreta do App. **Conta de terceiro: ele mesmo cadastra, por `POST /v1/cadastro`** |
| `VerifyToken` | string arbitrária; responde o GET de verificação do webhook | **o CLI sorteia e imprime** (ou você passa em `ZAPGW_VERIFY_TOKEN`), e você repete o valor na config do webhook na Meta |
| `SendToken` | token **permanente** de System User; fala com a Graph API | Business Settings → Usuários do sistema (ver Fase 4). **Conta de terceiro: ele mesmo cadastra, por `POST /v1/cadastro`** |
| `CallbackURL` | para onde o zapgw entrega ao consumidor | **não** é da Meta — é do sistema consumidor |
| `DeliverySecret` | assina o POST do zapgw ao consumidor | **o CLI sorteia e imprime** (ou você passa em `ZAPGW_SEGREDO_ENTREGA`), e o consumidor põe o valor no `.env` dele |

Os três primeiros são **identificadores** (não segredos). `AppSecret` e `SendToken` são **segredos** e
seguem a regra do projeto: nunca no Git, transporte por `C:\dev\github\secrets-transfer\`.

### Dois destes segredos são COMPARTILHADOS, e por isso o CLI os mostra (T-052)

`zapgw provisionar instancia` sorteia todo segredo que não vier por variável de ambiente. Para
`app_secret` e `token_envio` ele diz apenas **quais** sorteou, nunca o valor — ninguém precisa
lê-los de volta.

`verify_token` e `segredo_entrega` são diferentes: o provisionamento **só termina quando o valor
chega a uma pessoa** (você digita o primeiro no painel da Meta; o consumidor põe o segundo no `.env`
dele). Por isso, **quando são sorteados, o comando os imprime uma vez**, com o aviso de que o gateway
guarda só o cifrado e não os mostra de novo:

    GUARDE AGORA os valores abaixo — o gateway guarda so o cifrado e NAO os mostra de novo.
    verify_token: <64 hex>
    segredo_entrega: <64 hex>

**O que acontecia antes, e é o motivo desta seção existir:** os dois eram sorteados em silêncio. A
instância nascia, `zapgw instancia mostrar` dizia `verify_token=sim segredo_entrega=sim` — parecia
completa — e era **impossível concluir o provisionamento**, porque nada é decifrado de volta em
comando nenhum. Sem erro apontando a causa: o sintoma aparecia dias depois, no painel da Meta, como
*"a verificação recusa"*, que manda procurar no lugar errado. Custou uma rotação extra na T-046
(2026-07-28).

**Se você passar o valor pelo ambiente, nada é impresso** — você já o tem. É o caminho de produção.

**Se perdeu o valor**, não há recuperação: gere outro e troque com
`ZAPGW_VERIFY_TOKEN=<novo> zapgw instancia rotacionar --slug <slug>` (o `rotacionar` **não** sorteia
— o valor vem do ambiente, exatamente para que quem rotaciona saiba qual é).

## As fases, em ordem, com o que trava o quê

### Fase 1 — Conta e App *(rápida, não depende de nada nosso)*
1. Ter uma **Meta Business account** (`business.facebook.com`).
2. Criar um **App Meta** (`developers.facebook.com`) do tipo Business e adicionar o produto
   **WhatsApp**. Isso já dá acesso à Cloud API no tier não-verificado.

> **Nenhum passo desta fase precisa do zapgw pronto nem no ar.** É rápido — não é a etapa longa.

### Fase 1b — Verificação do negócio *(longa, PARALELA, não bloqueia o lançamento)*
3. Submeter a **Business Verification**: a Meta confere a existência legal do negócio (documento da
   empresa, endereço, telefone verificável). É a etapa que mais demora, mas **destrava escala**, não
   habilita o envio — o envio já funciona no tier não-verificado sem ela.
   *[CONFERIR: documentos exigidos e prazo — variam por país e mudam.]*

> Comece esta em paralelo, mas **não** espere por ela para pôr o gateway no ar a 250/24h.

### Fase 2 — Número e nome de exibição *(registro rápido; revisão do nome à parte)*
4. Adicionar um **número de telefone** ao WABA. O número **não pode** estar ativo num WhatsApp comum
   ou no app WhatsApp Business — se estiver, tem de ser desvinculado antes. Precisa receber
   SMS/ligação para o código de verificação.
5. Definir o **nome de exibição**, que passa por **revisão da Meta** e pode ser **recusado** por
   política. *[CONFERIR a política de nome vigente antes de submeter — recusa recomeça a espera.]*
6. **Método de pagamento** no WABA: a Cloud API cobra por conversa acima da faixa gratuita.
   *[CONFERIR a faixa/tarifa vigente.]*

### Fase 3 — Token permanente de produção *(rápido, mas fácil de errar)*
7. O token que o painel dá de cara é **temporário (expira em ~24h)** *[CONFERIR]* e **não serve** para
   produção. Em **Business Settings → Usuários do sistema**, crie um **System User**, dê a ele acesso
   ao WABA e ao App, e **gere um token permanente** com os escopos `whatsapp_business_messaging` e
   `whatsapp_business_management` *[CONFERIR os escopos exatos]*. Esse é o `SendToken`.
8. Guarde o **App Secret** (App → Configurações → Básico). É o `AppSecret`, usado para verificar o HMAC
   dos webhooks de entrada.

### Fase 4 — Webhook *(a ÚNICA fase que depende do zapgw implantado — plano 4)*
9. Na config do WhatsApp do App, aponte o **Callback URL** para
   `https://zapgw.<dominio>/v1/inbound/<slug>` e o **Verify Token** para o valor que
   `zapgw provisionar instancia` imprimiu (ou o que você passou em `ZAPGW_VERIFY_TOKEN`) — ver
   *"Dois destes segredos são COMPARTILHADOS"*, acima. A Meta faz um `GET` de desafio na hora — o
   zapgw já responde a isso (`GET /v1/inbound/{slug}`).
10. **Salvar a Callback URL já inscreve um CONJUNTO de campos — o seu trabalho aqui é revisar, não
    assinar.** E, se rotear por número, configure o **override de webhook por número/WABA** — sem
    isso, um App com vários números entrega tudo num endpoint só, e o zapgw recusa o lote misturado
    (ver `docs/ARMADILHAS.md` e o contrato).

    > **Este passo dizia "assine o campo `messages`", como se fosse ação manual e única. Não é** —
    > corrigido em 2026-07-28 (T-056) contra o comportamento observado. Ao salvar a Callback URL
    > nova, o App foi inscrito de uma vez num conjunto padrão de campos, **sem ninguém escolher**:
    > **dez**, na medição daquele dia, contra o **um** que este doc mandava assinar. Quem seguisse a
    > frase antiga esperaria um campo e teria dez ligados sem saber.

    **Como saber o que está inscrito de verdade** — é a única forma, e o painel não mostra isso de
    maneira óbvia:

    ```
    GET https://graph.facebook.com/v25.0/{app-id}/subscriptions?access_token={app-id}|{app-secret}
    ```

    O token é o **app access token**, que é literalmente `app_id|app_secret` com a barra vertical no
    meio. **Ele é credencial de administrar as inscrições do App** — quem o tem reaponta a Callback
    URL e desvia todo o tráfego —, então trate-o como segredo e não o deixe entrar em log, histórico
    de shell ou transcript (ver `docs/ARMADILHAS.md`, *Meta / WhatsApp Cloud API*). O `GET` é
    **leitura e não muda nada**; a versão `v25.0` é a mesma que o gateway usa por default
    (`graphBase`, `cmd/zapgw/main.go`).

    > 🔴 **De onde o `app_secret` NÃO sai: da máquina que roda o gateway.** Medido em 2026-07-28
    > (T-057): ele **não está** em `/etc/zapgw/env` no CT 125 — aquele arquivo tem
    > `ZAPGW_CHAVE_CIFRA`, `ZAPGW_BANCO` e `ZAPGW_ENDERECO`. O `app_secret` mora **cifrado no banco,
    > por instância**, e **nenhum comando do CLI o decifra de volta** — o que é decisão, não lacuna:
    > a T-052 escolheu imprimir só os dois segredos que precisam existir FORA do gateway
    > (`verify_token` e `segredo_entrega`), e este não é um deles. Consequência prática: **quem for
    > rodar este `GET` precisa do valor de outra fonte**, e não adianta procurar no CT. Se você não
    > tem o valor, **diga que não mediu** — não deduza a lista de inscritos a partir do que a doc
    > dizia ontem.

    **O que fazer com o resultado:** conferir contra `docs/META-CAMPOS-DE-WEBHOOK.md`, que traz o
    inventário medido em 2026-07-28 — quais campos estavam inscritos, quais o gateway modela, e a
    forma do payload de cada um. **O número não é estável nem é o mesmo para todo App**: só o `GET`
    responde pelo *seu* App, hoje.

    **Podar é decisão do dono do negócio, não deste doc e não de quem implanta.** Campo inscrito e
    não modelado **não é erro**: ele chega, passa as guardas e é entregue ao consumidor sem evento
    nenhum (`"eventos": []` — ver `docs/CONTRATO-CONSUMIDOR.md`), virando linha crua no banco dele
    — volume e dado guardado sem uso, não risco. E
    **desinscrever tem custo assimétrico**: o campo que ninguém lê hoje pode ser o que avisaria de
    uma queda de qualidade amanhã. Levante a lista, mostre ao dono, e **não desinscreva nada por
    conta própria** — muito menos no mesmo dia de uma virada, quando acrescentar variável ao que
    acabou de estabilizar é o pior negócio disponível.

    **ACRESCENTAR um campo depois é ação de painel, não de API — e a distinção não é gosto** (T-057,
    2026-07-28, quando `template_category_update` foi inscrito). A rota
    `POST /{app-id}/subscriptions` existe e a credencial acima serve para ela, mas ela recebe
    `callback_url` **e** `verify_token` **no mesmo pedido**: errar um caractere ali reaponta a entrega
    de todo o tráfego. O painel tem **alternador por campo, que não toca na Callback URL**. Numa
    instalação em produção, o caminho seguro vale mais que o automatizável — e o `GET` acima confere
    depois, porque ele não muda nada.
11. Este passo exige que `zapgw.<dominio>` **já esteja no ar com TLS válido no hostname público** — que
    é plano 4. Antes disso, a verificação do webhook falha **calada** dos dois lados (ver a armadilha
    de certificado). Por isso é a última fase, e a única que espera por nós.

## O sequenciamento que importa

```
Fase 1 (App) + Fase 2 (número) + Fase 3 (token)  ─┐  rápidas, da Meta
                                                   ├─► Fase 4 (webhook) ─► ATIVA a 250/24h
Planos 3–4 do zapgw (código + DEPLOY)  ────────────┘  ← este é o caminho crítico

Fase 1b (verificação do negócio, ~20–40 dias) ──────► destrava tiers acima de 250 (paralela)
```

**Priorize o deploy; comece a verificação em paralelo para ela não virar gargalo *depois*, quando o
volume subir.**

## O que só o dono pode fazer (não é código, não delego)

Verificação do negócio, escolha e verificação do número, nome de exibição, método de pagamento e a
criação do System User são atos da **identidade do negócio** na Meta — exigem os documentos e as
credenciais do dono. O meu papel é preparar o zapgw para receber os valores que essas etapas produzem
(o painel de administração, plano 4) e conferir o fluxo pelo teste de fumaça. **Iniciar o onboarding é
uma ação sua.**
