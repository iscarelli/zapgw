# Modelo de uso do zapgw — quem faz o quê

*[Read in English](MODELO-DE-USO.md)*

**Código:** `internal/outbound/registration_handler.go` (o passo 3, `POST /v1/cadastro`),
`internal/outbound/smoke.go` (o caminho único dos quatro passos do fumaça, chamado pelas DUAS
fachadas), `internal/outbound/smoke_handler.go` (`POST /v1/fumaca`, o passo 4 por API),
`internal/outbound/pause_handler.go` (`POST /v1/pausa`), `cmd/zapgw/smoke.go` (a fachada de linha
de comando do mesmo caminho), `internal/config/store.go` (`RegisterMeta`, `RegistrationWindow`,
`ReopenRegistrationWindow`, a migração `instancia.cadastro_em`, e `ActivateInstance`/`PauseInstance`
— os únicos caminhos para `ativo`), `cmd/zapgw/provision.go` (criação só com `--slug`, o pacote de
entrega e `zapgw instancia reabrir-cadastro`), `cmd/zapgw/state.go` (a linha que diz onde a série
diária mora), `internal/meta/instagram.go` (o envio e o parse de Instagram). Os passos 1, 2, 3, 4 e
o item 8 saíram na T-079; o
item 1 (criação manual) e o 7 (cadastrar não ativa) são anteriores e continuam valendo. O passo 4
por API (item 7 da auditoria, abaixo) saiu na T-084; os itens 2, 3, 4 e 5 da auditoria saíram na
T-083. A seção *Instagram* saiu na T-097.

Este documento é a **fonte do desenho**; as tarefas e o manual derivam dele. Decidido pelo dono em
2026-07-28. **Se este arquivo divergir de uma tarefa, ele vence e a tarefa está errada.**

Ele existe porque o desenho foi decidido numa conversa rápida e o planner o reconstruiu errado três
vezes seguidas — inclusive invertendo a direção de uma API inteira. **Desenho que só existe em chat
não existe.**

## O alvo

**Programadores TERCEIROS**, com conta Meta própria, **fora do alcance do dono e sem canal para
perguntar**. Não é "outro sistema meu": é gente que recebe credencial uma vez e se vira com a
documentação.

> *"Ideal é ser o B, posso não conseguir subir esse canal sempre."*

## O fluxo, e quem faz cada passo

| # | Quem | O quê |
|---|---|---|
| 1 | **Dono** | Cria a instância. **Manual, sempre.** Fornece o **slug** — *"se não, usuário faz aberração"*. O slug é imutável e vira caminho de URL. |
| 2 | **Dono** | Entrega ao consumidor **o mínimo para ele falar com o gateway**. Nada além. |
| 3 | **Consumidor** | Cadastra **a Meta dele** no gateway, **por API**, a partir do painel dele. |
| 4 | **Consumidor** | Prova o canal (`fumaca`) — só isso ativa a instância. |
| 5 | **Consumidor** | Opera: envia, recebe, lê estado — **tudo pelo gateway**. |

**A direção do passo 3 é CONSUMIDOR → GATEWAY.** É escrita, não leitura. *(O planner desenhou uma
rota de autodescrição — o gateway devolvendo dados — duas vezes, e estava errado nas duas.)*

## O que está DECIDIDO

1. **A criação é manual e sempre será.** Sem auto-provisionamento, sem sandbox, sem segredo derivável
   do nome do slug. Considerado e descartado.
2. **O slug é do dono.** Não é escolha do integrador.
3. **O consumidor tem a Meta dele.** O dono não conhece, não guarda e não intermedia `waba_id`,
   `phone_number_id`, `app_secret` nem `token_envio` — **isso é dado do consumidor**.
4. **O consumidor tem o painel dele, e a segurança dele é problema dele.** Nós não construímos painel.
   *"E continuamos sobrevivendo sem painel."*
5. **Ninguém fala direto com a Meta** — nem para leitura. Ver `CLAUDE.md`.
6. **Segredo entra e não volta.** Grava cifrado; nenhuma superfície o decifra para exibir.
7. **Cadastrar não ativa.** A instância nasce pausada; só um envio bem-sucedido a ativa.
8. **A janela de cadastro é de 24 h, contadas da PRIMEIRA inserção do consumidor.** Depois disso a
   configuração **trava**, e mudá-la exige intervenção humana do dono.
   > *"24h da primeira inserção: eu criar a instância hoje, daqui 5 dias o consumidor insere algo,
   > começa a contar ali."*
   **Não conta da criação da instância** — um consumidor lento perderia a janela antes de começar. E
   **não reinicia a cada mudança** — se reiniciasse, quem mexesse todo dia manteria a janela aberta
   para sempre, e a regra viraria decorativa.
   **O que ela resolve, e é elegante:** o token de consumidor é poderoso **por um tempo**, não por
   permissão. Durante a janela ele testa, erra e corrige sozinho; depois dela, um token roubado volta
   a valer só "manda mensagem", que é o risco que já existia. *Limitar no tempo em vez de limitar na
   permissão foi decisão do dono, e é melhor que o que o planner tinha proposto.*
9. **O dono tem comando para REABRIR a janela.** Consumidor que travar com credencial errada vai
   acontecer, e sem comando a saída seria SQL na mão em produção — que é exatamente o que a T-048
   existiu para matar.

## O que isso IMPLICA, e não é opcional

- **O token de consumidor deixa de ser "manda mensagem" e passa a ser "reconfigura a instância".**
  Quem o roubar repõe credencial e aponta a instância para a Meta dele. É consequência aceita do
  modelo — e **o consumidor tem de saber**, porque a proteção do lado dele é dele e ele precisa
  dimensionar.
- **A exigência de `waba_id`/`phone_number_id` muda de lugar, não some.** A T-074 passou a exigi-los
  na criação; com este modelo, eles passam a ser exigidos **no cadastro**. O teste dela se ajusta, não
  se remove.
- **Uma instância por App da Meta.** A separação real entre inquilinos é a **assinatura por
  instância**, e ela só distingue quando os `app_secret` são diferentes. Com terceiros trazendo App
  próprio isso é garantido por construção — dois números do mesmo App derrubariam a garantia.
- **Sem canal, a documentação e as mensagens de erro SÃO o suporte.** Todo erro terminal ambíguo é
  um beco sem saída. Foi isto que transformou a T-078 de "arrumar um caso feio" em padrão.

## O que foi DECIDIDO em 2026-07-28, sobre a auditoria do contrato

As cinco primeiras estavam em aberto e o dono decidiu todas na mesma conversa.

1. ✅ **EXPOR as rotas de saída — mas NÃO hoje, e com URL DIFERENTE.**
   > *"Vamos expor, mas não hoje. Com URL diferente."*
   **É a T-053** (separar público de interno por **hostname**, não por porta), que passa de "avaliar" a
   **aprovada em princípio, sem data**. *A separação por porta que existe hoje protege por acidente de
   topologia, não por desenho — e este gateway é feito para rodar fora do homelab um dia.*
   ⚠️ **Enquanto não acontecer, o modelo de terceiros NÃO FUNCIONA**, e o contrato tem de dizer isso em
   voz alta em vez de descrever como pública uma superfície que não é. O caso mais caro é o
   `POST /v1/cadastro`: por ele passam o `app_secret` e o `token_envio` **do consumidor**, e ele é
   inalcançável — um terceiro não consegue nem começar.
2. ✅ **O contrato INTERNO passa a ser publicável — um documento só, sem versão derivada.**
   Dois documentos divergem, e é o que este projeto mais persegue. Saem as referências a `T-0xx` e os
   ponteiros `arquivo:linha`: a auditoria mostrou que **nenhuma** delas acrescenta informação que um
   terceiro possa usar (a data já está do lado, e o código ele não tem).
3. ✅ **NÃO existe endereço de contato, e o contrato diz isso uma vez, no topo.**
   As nove promessas implícitas (*"peça"*, *"avise"*, *"diga aqui"*) viram instruções que se resolvem
   sozinhas. *Acrescentar um canal depois é fácil; tirar um que as pessoas passaram a usar, não.*
4. ✅ **Depreciação por PRAZO, não por consenso.** *"Campo marcado obsoleto sai no mínimo N meses
   depois, anunciado em Mudanças que quebram."* A condição antiga — *"os dois consumidores confirmarem
   por escrito"* — nunca fecha com N terceiros, e deixa o integrador lendo "OBSOLETO" sem saber se pode
   usar.
   **N = 6 meses**, escolhido e escrito na T-083 (`docs/CONTRATO-CONSUMIDOR.md`, seção *Política de
   depreciação*). Se o dono quiser outro número, é ali que ele muda — e a tabela de datas mínimas
   (`dia`, `serie_7_dias`) muda junto.
5. ✅ **Números reais saem dos exemplos**, substituídos por convenção declarada. Conserta junto o que a
   auditoria achou: os exemplos alternam entre dois slugs e **um deles é o real** — para quem não pode
   perguntar, marcador que parece real é cara-ou-coroa.
6. ✅ **`serie_diaria` fica FORA do `zapgw estado`** (decisão do dono: *"mantenha fora"*). Dezenas de
   linhas por instância tornariam o terminal inútil, e a série curta continua na tela, então o operador
   não fica cego. **A T-083 acrescentou duas linhas ao `zapgw estado` dizendo que ela existe e por onde
   sai** — omitir sem dizer onde mora era o defeito da T-065 com o sinal trocado.

**Os itens 2, 3, 4 e 5 saíram na T-083** (`docs/CONTRATO-CONSUMIDOR.md`, e `cmd/zapgw/state.go` para
o complemento do item 6).

7. ✅ **O passo 4 passa a ser executável pelo consumidor: o FUMAÇA ganha rota, e a PAUSA também.**
   Decidido pelo dono em 2026-07-28, 21:21, sobre o buraco levantado ao implementar a T-079 — o
   `zapgw fumaca` é **comando de linha**, um terceiro não tem shell na máquina do gateway, e não
   havia canal nem para ele avisar que cadastrou. **Implementado na T-084**
   (`internal/outbound/smoke.go`, `smoke_handler.go`, `pause_handler.go`).

   **O verbo é o que faz a decisão funcionar, e ele não é "ativar":**

   | Rota | O que faz | Por que pode existir |
   |---|---|---|
   | roda o **fumaça** | envia de verdade à Meta e, **se a Meta aceitar**, ativa | `ativo = 1` continua sendo **consequência da prova**, não do pedido |
   | **pausa** | volta a `ativo = 0` | sentido seguro (fail-closed); a volta **exige fumaça novo** |

   🔴 **O que continua não existindo, nem por rota: desligar a EXIGÊNCIA do fumaça.** Isso é a flag de
   força com outro nome. `internal/config/store.go` (comentário de `ActivateInstance`) (*"AtivarInstancia e o UNICO caminho para
   `ativo = 1` neste projeto"*) e `internal/outbound/smoke.go` (*"NAO EXISTE FLAG DE FORCA"*) existem
   porque um caminho para `ativo = 1` sem tráfego real deixaria o consumidor cadastrar credencial
   errada, apertar o botão e descobrir no primeiro cliente de verdade. **A rota também não manda
   mensagem quando a instância já está ativa** — senão ela viraria o único jeito de gastar mensagem
   paga em loop, já que é a única rota do gateway que envia sem o consumidor ter pedido um envio.

   ⚠️ **Isto NÃO destrava o modelo sozinho:** a rota nasce ao lado das outras rotas de saída, então
   continua inalcançável para um terceiro **até a T-053** — exatamente como o item 1.

## Instagram (T-097, 2026-07-30) — o MESMO alvo, um modelo DIFERENTE

A instância ganha **tipo**: `whatsapp` (o default — tudo acima descreve ELE, sem mudar nada) ou
`instagram`. Instagram usa o modelo **ANTIGO**, de antes da T-079 — os itens 1 (criação manual) e
7 (cadastrar não ativa) desta página, sem os itens 2–6 (o cadastro por API do consumidor,
`POST /v1/cadastro`).

**Por quê, e por que isso não é regressão:** o item 3 desta página ("o consumidor tem a Meta dele")
continua verdadeiro — quem traz o `ig_id` e as credenciais é o dono do canal, não o dono do gateway.
O que muda é o **CANAL** por onde essas credenciais chegam ao gateway: para WhatsApp, é uma chamada
HTTP (`POST /v1/cadastro`) que o consumidor faz depois de a instância existir; para Instagram nesta
fatia, é uma flag de linha de comando (`zapgw provisionar instancia --tipo instagram --ig-id
<IGID>`) que o DONO digita, com as credenciais vindas do consumidor por fora (o mesmo canal humano
que hoje entrega o pacote de provisionamento).

Não escrevemos um `RegisterMeta` equivalente para Instagram — não porque a necessidade seja
diferente, mas porque esta é a **primeira fatia**, e replicar o modelo inteiro (janela de 24h,
`ReopenRegistrationWindow`, validação de identificação por API) sem um segundo consumidor pedindo
seria construir para uma demanda hipotética. Se um terceiro real precisar se autocadastrar numa
instância Instagram, essa é a próxima tarefa — e ela extenderia `MetaRegistration` e
`internal/outbound/registration_handler.go` para o tipo novo, não criaria um caminho paralelo.

**O que NÃO muda, e é o que este documento existe para proteger:** a instância nasce **pausada**
(`CreateInstance` grava `ativo = 0` para qualquer tipo — a checagem é estrutural, não por campo), e
só um envio de teste **aceito pela Meta de verdade** a ativa (`zapgw fumaca` / `POST /v1/fumaca`,
estendidos para chamar `SendInstagramMessage` quando o tipo pedir — `internal/outbound/smoke.go`).
Não existe, e não pode passar a existir, uma flag de força para nenhum dos dois tipos.

Detalhes de protocolo (IGSID, janela de 24h/7 dias, o que a rota de envio aceita) estão em
`docs/CONTRATO-CONSUMIDOR.md`, seção *Instagram — a primeira fatia* — este documento descreve
**quem faz o quê**, não a forma dos corpos.

## O que CONTINUA em aberto

Nada do desenho de WhatsApp. O que falta lá é alcance de rede (item 1 → T-053).

Do lado do Instagram: um `POST /v1/cadastro` equivalente (se um terceiro real vier a precisar),
mídia/template/reação/marcação de leitura (fora de escopo desta fatia, não decidido como "nunca"),
e alcance de rede — a mesma T-053, porque a rota é a mesma porta.
