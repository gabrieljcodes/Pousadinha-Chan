# Gacha Pousadinha

Updated player commands, roll pools, divorce payouts and trades: [English command guide](GACHA_COMMANDS.md).

Gacha com catálogo global e coleções exclusivas por servidor. Integração em Go/PostgreSQL, comandos exclusivamente slash, sem dependência das APIs externas durante o jogo. O recurso começa **desabilitado**, com catálogo vazio. Não altera saldos existentes.

## Jogabilidade implementada

- `/roll`: sorteios com pools por gênero e tipo de obra.
- **Claim character**: botão para reservar o personagem durante 45 segundos.
- `/harem`: coleção pessoal ou de outro membro, em lista ou fotos.
- `/search`, `/series` e `/info`: pesquisa de personagens, busca por obras/animes e detalhes de personagens.
- `/gallery`: imagens aprovadas.
- `/wishlist`: 5 slots iniciais e +2% de chance de spawn para personagens desejados.
- `/profile`: cotas e disponibilidade de claim.
- `/keys`, `/top`, `/harem-ranking`, `/trade`, `/gift`, `/divorce` e `/offers`: progressão, rankings e gerenciamento da coleção.

Nomes, opções e mensagens do bot usam inglês via go-i18n. Consulte o [guia de comandos](GACHA_COMMANDS.md) e a [documentação de tradução](I18N.md).

Sorteios têm chances uniformes entre personagens habilitados com ao menos uma imagem aprovada. Personagens já possuídos continuam no sorteio, mas não podem ser reservados de novo. Favoritos são metadados de popularidade, não um valor monetário. Sorteios não cobram moedas. Agora há divórcio com pagamento, trocas e presentes com aceite; veja o guia de comandos acima.

Reservas, cotas e deduplicação de eventos ficam no banco. Transações, bloqueios por jogador e restrição única por servidor/personagem protegem operações concorrentes, inclusive com mais de uma instância. Rejeição HTTP explícita pelo Discord estorna o sorteio; timeout de transporte é ambíguo e não estorna automaticamente. Expiração é aplicada no banco mesmo quando um botão antigo ainda aparece habilitado.

## Ativação

1. Inicie o bot normalmente para criar o schema base (`users`). Faça backup do banco antes de atualizar a instalação.
2. Configure `.env`:

```dotenv
ENABLE_API=true
GACHA_ENABLED=true
GACHA_MEDIA_DIR=/app/data/gacha
GACHA_PUBLIC_URL=https://pousadinha.com
GACHA_ROLLS_PER_HOUR=10
GACHA_CLAIM_HOURS=3
GELBOORU_API_KEY=
GELBOORU_USER_ID=
```

3. `docker compose up -d --build bot`. O Dockerfile inclui FFmpeg e o executável administrativo `/app/gacha`; o Compose monta `gacha_media` em `/app/data/gacha`.
4. Configure DNS e um proxy HTTPS para a porta HTTP do bot. Exemplo de **Caddyfile**, ajustando o endereço ao ambiente:

```caddyfile
pousadinha.com {
    reverse_proxy 127.0.0.1:8080
}
```

O domínio precisa ser acessível pelo Discord. A configuração exige uma origem HTTPS, sem caminho, query ou credenciais. O código não compra domínio, configura DNS ou instala certificados automaticamente. `/api/v1/...` continua usando as rotas existentes; os caminhos de mídia são atendidos pelo mesmo servidor.

Uma URL gerada tem este formato:

```text
https://pousadinha.com/naruto/42-naruto-uzumaki/photo-gelbooru-123456/<sha256>-v1.png
```

O ID interno evita colisões entre homônimos; o hash e a versão identificam o conteúdo/renderizador. URLs persistidas não mudam quando o nome do personagem é atualizado. Somente arquivos aprovados são públicos; não configure um servidor estático apontando diretamente para todo o volume, pois isso exporia pendências e rejeições. Cache HTTP de cinco minutos limita a demora de revogações; caches externos do Discord podem persistir por mais tempo.

Para desenvolvimento fora do Docker, use Go 1.25+ e FFmpeg no PATH, com `GACHA_MEDIA_DIR=./data/gacha`. O processador aceita originais PNG/JPEG/GIF, gera PNG ou GIF e não mantém o download bruto após o processamento.

## Curadoria e importação

A ferramenta administrativa usa as credenciais de banco do bot; não há endpoint HTTP público de escrita do catálogo. Execute um importador por vez para respeitar a limitação externa (o controle de taxa é por processo).

```bash
# Importação direcionada: ID de um personagem do AniList.
docker compose exec bot /app/gacha import 17
# Anote o ID INTERNO do personagem e do retrato retornados.

# Associe explicitamente a tag exata do personagem no Gelbooru.
docker compose exec bot /app/gacha tag 42 naruto_uzumaki

# Escolha um post específico do Gelbooru; não é um crawler de imagens.
docker compose exec bot /app/gacha image 42 123456

# Liste caminhos locais e referências das pendências.
docker compose exec bot /app/gacha pending

# Após examinar o arquivo local, aprove a imagem e habilite o personagem.
docker compose exec bot /app/gacha approve 7 nome-do-revisor
docker compose exec bot /app/gacha enable 42

# Revogar publicação ou pausar novos sorteios.
docker compose exec bot /app/gacha reject 7 nome-do-revisor
docker compose exec bot /app/gacha disable 42
```

**IDs acima são ilustrativos e de espaços diferentes**: AniList, catálogo interno, post do Gelbooru e imagem interna. Use os IDs efetivamente retornados. `import` cria/atualiza metadados e baixa o retrato inicial; a imagem fica pendente. Se o download falhar, os metadados permanecem e a operação pode ser repetida. Reimportação preserva coleção, moderação e associações de outros provedores. Uma imagem da mesma fonte/ID já cadastrada é reutilizada; não há atualização automática do binário remoto.

Use the authenticated [catalog editor](CATALOG.md) to preview and approve pending images on either backend. Approval and character enablement remain independent. Disabling a character prevents new rolls; rejecting a photo removes public access without deleting existing collections.

O filtro do Gelbooru exige tag exata e classificação `general/safe` tanto na consulta quanto na resposta. Isso não substitui revisão humana. Guarde e respeite os créditos/licenças da publicação; uma API de imagens não concede automaticamente direitos de redistribuição. A página de origem e a referência original do post ficam no banco; o cartão liga para a publicação com os créditos.

## Arquitetura e crescimento

Migration aditiva: [`004_gacha.sql`](../migrations/004_gacha.sql), embutida no binário. Aplica em transação com bloqueio de migração; falhas impedem a ativação. Não apaga nem converte tabelas da economia. RLS sem políticas públicas: conexão do bot precisa ser proprietária das tabelas ou usar papel com BYPASSRLS. Credenciais `anon`/browser não devem acessar esse schema.

- `gacha_characters`: identidade interna, nomes alternativos, gênero do personagem, descrição, favoritos, estado editorial.
- `gacha_character_sources`: IDs por provedor, URLs e data de sincronização. Não mescla personagens apenas pelo nome.
- `gacha_works` e `gacha_character_works`: relação muitos-para-muitos, papel do personagem, tipo `anime/manga/game/other`. Gêneros e nomes de estúdios são arrays JSONB por obra; podem ser normalizados em entidades próprias quando houver necessidade de busca analítica ou IDs de organizações.
- `gacha_booru_tags`: associação revisável por provedor.
- `gacha_assets`: origem, hash, MIME, caminho, tamanho padronizado, decisão de revisão e responsável. Arquivos são gravados atomicamente antes de serem cadastrados.
- `gacha_players`, `gacha_rolls`, `gacha_collection`, `gacha_wishes`: estado de jogo persistente, limites e isolamento por servidor.

`CatalogProvider` e `Character`/`Work` definem o contrato de um novo provedor de jogos. A importação não presume que todos os personagens são de anime. Para cruzar uma identidade existente entre provedores será necessário um fluxo explícito de associação/mesclagem; importar outro provedor cria outra identidade até que essa associação seja feita. O único provedor de metadados entregue é AniList, e o de imagens complementares é Gelbooru.

O AniList retorna favoritos, gênero do personagem e obras de anime com gêneros e estúdios principais. A consulta pagina todas as relações retornadas para o personagem solicitado, filtrando obras adultas. A importação individual não faz varredura de IDs. O novo modo em lote descobre páginas por popularidade; veja a seção de lote e a restrição de coleta massiva abaixo. As [regras do AniList](https://docs.anilist.co/guide/terms-of-use) proíbem coleta massiva; para projetos maiores, alinhe o uso com o provedor ou use uma fonte que autorize distribuição do catálogo. O cliente espaça requisições em 2,2 s, aplica tentativas limitadas para 429/5xx e respeita `Retry-After`, considerando o [limite degradado documentado de 30/min](https://docs.anilist.co/guide/rate-limiting). Autenticação do Gelbooru segue sua [documentação](https://gelbooru.com/index.php?page=wiki&s=view&id=18780).

## Mídia e operação

Cartões **420 × 600 px**, proporção preservada sem recorte, fundo escuro, borda dourada dupla. GIFs: até 12 s, 12 fps, paleta otimizada e loop. Limites: 12 MiB de download, 24 milhões de pixels de entrada, 8 MiB de saída, prazo de 40 s para FFmpeg. HTTPS e hosts permitidos, validação do IP no momento da conexão, redirecionamentos limitados e armazenamento servido com proteção contra escape por symlink. Não há URLs externas de arquivo nos embeds.

Back up PostgreSQL together with the configured media store. Filesystem deployments need a shared media volume; S3 deployments use the same bucket and prefix across replicas. Imports can leave unreferenced objects if a database operation fails; those objects are not published. The private [catalog editor](CATALOG.md) handles curation. See [storage configuration, copying and operational limits](MEDIA_STORAGE.md) before switching backends.

## Verificação

```bash
go test ./...
# Banco PostgreSQL descartável, exclusivo para testes:
GACHA_TEST_DATABASE_URL='postgres://postgres:gacha-test@127.0.0.1:55439/gacha_test?sslmode=disable' go test -race ./...
```

Os testes de integração criam/removem seu próprio schema no banco de testes. Cobrem migração idempotente, reimportação, reservas concorrentes, cotas simultâneas, replay, expiração, isolamento de servidores/canais, persistência após reinício, estorno e bloqueio de imagens rejeitadas. Testes de renderização verificam dimensões, cor da borda e animação real. Sem `GACHA_TEST_DATABASE_URL`, testes de PostgreSQL são pulados; sem FFmpeg, os de mídia são pulados. APIs externas são simuladas: operação ao vivo ainda depende das credenciais, disponibilidade dos provedores e configuração Discord/DNS do ambiente.

## Importação inicial em lote

```bash
go run ./cmd/gacha import-batch --job initial-10k --limit 10000 --auto-approve
# Consultar progresso em outro terminal:
go run ./cmd/gacha batch-status initial-10k
# Retomar após uma falha, incluindo os itens que falharam:
go run ./cmd/gacha import-batch --job initial-10k --limit 10000 --auto-approve --retry-failed
```

No Docker, substitua `go run ./cmd/gacha` por `docker compose exec bot /app/gacha`. Para execução desacoplada do terminal, compile o binário e execute com `nohup` ou um serviço do sistema. Não execute a versão nova em paralelo com um binário antigo do importador. O mesmo nome de job exige as mesmas opções de quantidade e aprovação ao retomar.

O lote busca páginas de 50 IDs por favoritos e faz importações individuais com o mesmo cliente limitado a uma requisição a cada 2,2 s. Também respeita cabeçalhos de orçamento esgotado e `Retry-After`. Uma lease no PostgreSQL impede dois importadores em lote simultâneos; após encerramento abrupto, ela expira em até cinco minutos. Não rode importações individuais em paralelo, pois elas não usam a lease do lote.

O alvo é a quantidade de importações bem-sucedidas **neste job**, contando IDs existentes que foram atualizados. Personagens sem obra de anime elegível são ignorados; sem retrato ou com erro de download são falhas. A fila deduplica IDs entre páginas. Páginas ordenadas por popularidade podem mudar durante a execução, portanto não representam um snapshot exato do ranking. As páginas continuam até atingir o alvo ou esgotar a fonte. Erros consecutivos interrompem o lote após cinco personagens; erros de descoberta e HTTP 401/403 interrompem imediatamente. Se o AniList informar que a API foi desabilitada temporariamente, aguarde a restauração do serviço antes de retomar. Nenhuma falha é contada como importação concluída.

As tabelas `gacha_import_jobs`, `gacha_import_items` e `gacha_import_leases` guardam checkpoint, contagens, erro por personagem e exclusão entre processos. Aplicadas pela migration aditiva `005_gacha_import.sql`. Reiniciar não reprocessa itens concluídos. Um item interrompido antes do checkpoint pode ser tentado novamente, com upsert de metadados e reutilização da imagem. O job pode levar muitas horas: 10 mil requisições, sem contar páginas adicionais, já exigem cerca de 6,1 horas de espaçamento, além de rede e renderização.

`--auto-approve` aprova somente retratos identificados como AniList, após download, renderização e verificação do objeto no armazenamento configurado, registra `auto:anilist-portrait:v1` e habilita o personagem. Não aprova booru nem revoga rejeições manuais. `disable` agora grava uma trava editorial para impedir reativação automática; `enable` a remove. Personagens desabilitados antes dessa migration precisam receber `disable` novamente para registrar essa trava. Autoaprovação não é análise do conteúdo da imagem: origem AniList não garante que toda imagem seja adequada a todo servidor.

**Rate limit e permissão de coleta são condições diferentes.** Os termos do AniList proíbem coleta massiva, mesmo em baixa taxa; o importador não transforma um lote de 10 mil em uso autorizado pelo provedor. Essa restrição permanece aplicável: [termos](https://docs.anilist.co/guide/terms-of-use).

Arquitetura de execução, Valkey opcional e benchmarks: [Gacha runtime](GACHA_RUNTIME.md).

Media storage supports filesystem and private S3-compatible buckets. See [media storage configuration and migration](MEDIA_STORAGE.md).
