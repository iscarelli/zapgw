# zapgw

*[Read in English](README.md)*

Gateway para as APIs de mensagem da Meta (WhatsApp Cloud API e Instagram DM) — multi-inquilino,
binario estatico, sem dependencia externa em tempo de execucao.

Um consumidor manda `POST /v1/messages` e recebe de volta um `POST` assinado no `callback_url` dele
quando algo chega. Quem fala com a Meta e' o gateway; quem decide **o que** e **quando** enviar e' o
consumidor.

## Estado

**Em producao desde julho de 2026, com dois consumidores.** O codigo chegou aqui em 2026-08-30, vindo
do repositorio privado onde o projeto nasceu: este passa a ser o repositorio de trabalho, e o antigo
congela como historico.

> **Por que o historico comeca em 2026-08-30 e nao em julho:** o repositorio de origem carrega, em
> centenas de commits, telefone de pessoa real e nome de cliente. **Nao existe despublicar.** Entao o
> projeto nasceu de novo aqui, com o primeiro commit ja higienizado, em vez de reescrever setecentos
> commits e torcer para nao ter sobrado nada. O custo dessa escolha e' o historico curto; o ganho e'
> que nenhum dado de terceiro precisa ser tirado depois.

## O que ele faz

- **Envio** — texto, midia, template, interativo, reacao, localizacao e contatos. **Uma acao da Meta
  por chamada:** laco, ordem e politica de repeticao sao do consumidor, que tem o contexto para
  conduzi-los.
- **Recebimento** — o webhook da Meta chega aqui, tem a assinatura conferida, e' isolado por
  inquilino, e e' reentregue **assinado** no `callback_url` do consumidor.
- **Multi-inquilino de verdade** — cada instancia tem token, segredo e destino proprios, cifrados em
  repouso. Toda rota declara como isola inquilino, e ha teste que reprova rota nova que nao declare.
- **Observacao** — contadores por instancia e por dia, log de transito, e um `/v1/estado` que
  distingue *"medi e esta ruim"* de *"nao consegui medir"*.

## Comecando

Requisitos: Go 1.22+ e um App da Meta com WhatsApp Business.

    git clone https://github.com/iscarelli/zapgw.git && cd zapgw
    CGO_ENABLED=0 go build ./cmd/zapgw
    cp .env.example /etc/zapgw/env      # preencha; NUNCA commite valores

O passo a passo do lado da Meta — App, numero, token permanente, webhook — esta em
[`docs/ONBOARDING-META.md`](docs/ONBOARDING-META.md), com o custo de cada etapa e o que trava quando.

## Implantando

O binario e estatico e nao tem dependencia em tempo de execucao. Duas formas de obter:

    # baixar do release (Linux; ha tambem zapgw-linux-arm64)
    curl -fsSLo zapgw https://github.com/iscarelli/zapgw/releases/download/v0.60.1/zapgw-linux-amd64
    chmod +x zapgw

    # ou compilar
    CGO_ENABLED=0 go build ./cmd/zapgw

No host de destino, tres coisas precisam estar no lugar:

| o que | onde | de onde vem |
|---|---|---|
| o binario | `/usr/local/bin/zapgw` | o release ou o build acima |
| a unit do systemd | `/etc/systemd/system/zapgw.service` | [`implanta/zapgw.service`](implanta/zapgw.service) |
| as variaveis | `/etc/zapgw/env`, modo `0600` | copia de [`.env.example`](.env.example), preenchida |

O `/etc/zapgw/env` e o unico lugar onde segredo mora — `ZAPGW_CHAVE_CIFRA` inclusive. Ele nao e
versionado, o deploy nao o copia, e nada disso passa por linha de comando.

### O script de deploy

[`implanta/deploy.sh`](implanta/deploy.sh) faz o caminho inteiro para um container Proxmox: compila,
envia, tira snapshot, guarda o binario anterior, troca, reinicia e **espera o `/v1/health`
responder**. Se ele nao responder no prazo, o script **reverte sozinho** para o binario anterior e
sai diferente de zero. Ele assume `pct` no no de destino; para outra topologia, vale mais como
roteiro do que como ferramenta.

**Tres variaveis sao obrigatorias e nao tem default.** A falta de qualquer uma para o script antes
de ele tocar em rede, nomeando a variavel e o formato esperado:

    ZAPGW_DEPLOY_VMID=100 \
    ZAPGW_DEPLOY_HOST=deploy@no-proxmox.exemplo.internal \
    ZAPGW_DEPLOY_SAUDE=http://<ip-interno-do-gateway>:8080/v1/health \
    implanta/deploy.sh

*Elas nao tem default de proposito.* Ate a abertura deste repositorio o script trazia o no, o
container e o IP de uma instalacao real embutidos. Num repositorio publico um default assim nao e
so um endereco vazado: e um script que, rodado por quem nao leu, tenta implantar num host que nao
e dele.

## Documentacao

- [`docs/CONTRATO-CONSUMIDOR.md`](docs/CONTRATO-CONSUMIDOR.md) — **a referencia da API.** Toda rota,
  todo campo, todo codigo de erro, e o que cada resposta promete e o que ela **nao** promete.
- [`docs/MANUAL-DO-INTEGRADOR.md`](docs/MANUAL-DO-INTEGRADOR.md) — do zero ate a primeira mensagem.
- [`docs/MODELO-DE-USO.md`](docs/MODELO-DE-USO.md) — o que e' responsabilidade do gateway e o que e'
  do consumidor, e por que a linha esta onde esta.
- [`docs/META-CAMPOS-DE-WEBHOOK.md`](docs/META-CAMPOS-DE-WEBHOOK.md) — os campos da Meta que
  atravessam, e os que nunca se traduzem.
- [`docs/ARMADILHAS.md`](docs/ARMADILHAS.md) — **leia antes de mexer em qualquer coisa.** Uma linha
  por pegadinha, cada uma com o custo real que ela cobrou. Nenhuma e' hipotetica.

## Regras que valem desde o commit inicial

- **Nada que identifique pessoa ou cliente real entra aqui** — telefone, nome de inquilino,
  identificador da Meta de terceiro, endereco de rede interna. Exemplo e fixture usam valor
  sintetico, **e ha um teste que reprova o contrario**, decodificando ate o base64 escondido dentro
  de um `wamid`.
- **TLS nao tem modo desligado**, em nenhum sentido e sob nenhuma flag — nem "so em desenvolvimento".
  Ha um teste que reprova quem introduzir um.
- O porque das duas, e o que faz cada uma falhar, esta em [`CLAUDE.md`](CLAUDE.md).

## Licenca

[AGPL-3.0-or-later](LICENSE). O produto e' um servidor: quem rodar uma versao modificada como servico
publica o fonte dela.
