# GitView

Monitor de alterações Git em tempo real no terminal. Cada arquivo modificado pode ser expandido ou recolhido individualmente, e os diffs se atualizam sozinhos. Inclui busca global nos diffs, exportação de relatórios, execução de testes, Code Flow e o Go Deep Inspector, para estudar código Go com evidências verificáveis.

```sh
go install .
gitview [opções] [diretório]
```

| Opção | Descrição |
|---|---|
| `--delta` | Renderiza diffs com git-delta (alternável com `d`) |
| `--expand` | Expande automaticamente arquivos novos |
| `--interval 2s` | Intervalo do polling de segurança |
| `--no-watch` | Desativa fsnotify e usa apenas polling |
| `--export-dir DIR` | Diretório padrão dos relatórios (também `GITVIEW_EXPORT_DIR`) |
| `--test-timeout 10m` | Tempo limite de uma execução de testes |
| `--ascii` | Símbolos ASCII (também `GITVIEW_ASCII=1`) |

## Atalhos

`?` mostra a ajuda da tela atual.

Mouse: a roda rola o diff; clicar numa dica do rodapé (por exemplo `x Exportar`) equivale a pressionar a tecla; na exportação, clicar num formato, escopo ou caixa `[x]` escolhe ou alterna a opção.

| Tecla | Ação |
|---|---|
| ↑↓ / j k, Tab / Shift+Tab | Navegar entre arquivos |
| Enter / Espaço | Expandir/recolher arquivo |
| e / c | Expandir todos / recolher todos |
| ← → / h l, 0 | Rolagem horizontal / voltar à coluna 1 |
| PgUp/PgDn, J/K, g/G | Rolagem vertical |
| `/` | Busca global |
| n / N | Próxima / anterior ocorrência |
| Esc | Limpar busca / voltar |
| `t` | Painel de testes |
| `f` | Code Flow: chamadores e chamadas de uma função Go |
| `i` | Go Deep Inspector: Data Flow, Execution Trace, Branch Explorer, Performance e SQL Explorer da mesma função |
| `m` | Runtime Performance: mede um processo Go em execução pelo PID |
| `a` | Repository Graph: o repositório inteiro como grafo navegável (exporta HTML) |
| `x` | Exportar relatório |
| d | Alternar renderização Delta/nativa |
| r | Atualizar agora (no painel de testes: reexecutar) |
| q | Sair |

## Busca global (`/`)

Busca em todos os diffs carregados, inclusive em arquivos recolhidos, por conteúdo, nome e caminho. Os resultados aparecem conforme você digita, com debounce de 120 ms, e o Git não é executado novamente: a busca usa os diffs já carregados, com um índice incremental que só reprocessa arquivos alterados.

- `Alt+C` alterna a diferenciação entre maiúsculas e minúsculas, e `Alt+R` liga as expressões regulares (sintaxe RE2). Uma regex inválida é informada na própria barra.
- `Enter` abre o arquivo selecionado na primeira ocorrência e expande **somente** esse arquivo.
- `n`/`N` percorrem as ocorrências de todos os arquivos, com rolagem vertical e horizontal até o trecho.
- As ocorrências recebem um fundo próprio e mantêm a cor da sintaxe. A ocorrência atual fica em amarelo.
- A busca e a posição atual são preservadas quando os diffs se atualizam.

Exemplo: `/` → `token_` → `↓` → `Enter` → `n n N`.

## Exportação (`x`)

Formatos **Markdown**, **HTML** e **JSON** (metadados, commit base, arquivos, hunks e linhas com números old/new, testes, comandos, logs e timestamps).

O relatório HTML, em inglês para ser anexado a Pull Requests, é um arquivo único que abre direto por `file://`:

- Cabeçalho com repositório, branch, commit base, data e escopo, além de uma linha de resumo (`7 files changed · 1238 additions · 2 deletions · Tests failed`).
- Navegação Overview / Files changed / Tests e uma lista de arquivos com status, nome, caminho, diffstat e link para o diff.
- Diff com números de linha antigos e novos em colunas fixas, syntax highlighting, cabeçalhos de hunk, rolagem horizontal só na área de código, e tabs e espaços preservados.
- Cada arquivo pode ser expandido ou recolhido, e há um link de âncora, "copiar caminho" e "copiar hunk" (em formato unified diff). Há também botões para expandir ou recolher tudo e um filtro por nome.
- Teclado: `/` filtra, `j`/`k` vão para o arquivo seguinte ou anterior, `o` alterna o arquivo atual, e `e`/`c` expandem ou recolhem tudo.
- Tema escuro e claro (a escolha fica salva), estilos de impressão (tema claro, tudo expandido) e layout para celular.
- Os testes aparecem como evidência: comando, duração, código de saída, resultados por pacote, falhas e o log completo expansível. Resultados de pacote (por exemplo, falha de compilação) não aparecem como testes individuais.
- Uma Content-Security-Policy bloqueia qualquer acesso à rede e só permite os scripts do próprio relatório, por hash. Todo conteúdo do repositório passa pelo escape do `html/template`.

Escopos: todos os arquivos, arquivo selecionado ou arquivos filtrados pela busca ativa.

- Nunca sobrescreve um arquivo existente sem confirmação: pressione `Enter` uma segunda vez para confirmar.
- Arquivos potencialmente sensíveis (`.env`, chaves, `credentials`, tokens AWS/GitHub/Slack, `password = "..."` etc.) têm o conteúdo **omitido** por padrão. Para incluí-lo é preciso marcar a opção explicitamente com `Espaço`.
- A gravação é atômica e roda em segundo plano, sem bloquear a TUI. Nada é enviado para serviços externos.
- Testes não executados aparecem como tais no relatório, sem resultados inventados.

## Testes (`t`)

Descoberta automática, que **não executa** nada:

- **Go**: `go.mod` e os pacotes com `*_test.go`. Módulos aninhados são tratados separadamente, e `vendor`, `testdata` e `node_modules` são ignorados.
- **Node.js**: `package.json` com Vitest ou Jest. Se houver apenas um script `test` declarado, ele é executado via npm, pnpm ou yarn, conforme o lockfile.

Alvos disponíveis: todos os testes, testes relacionados ao arquivo selecionado (o pacote Go do arquivo; para um `_test.go`, só as funções dele; no Node, `vitest related` / `jest --findRelatedTests`), cada projeto e cada pacote Go.

- Go usa `go test -json`, e os resultados vêm dos eventos estruturados (`run`, `pass`, `fail`, `skip`, `build-fail`), nunca de texto livre. Falhas de compilação e processos encerrados no meio de um teste são identificados.
- Vitest e Jest usam o relatório JSON (`--reporter=json` / `--json`). Scripts e comandos personalizados mostram apenas o código de saída e os logs, sem contagens inventadas.
- Estados: não executado, executando, aprovado, falhou, ignorado, cancelado e erro de infraestrutura. Um teste só aparece como aprovado depois de receber o resultado.
- `Enter` executa, `r` reexecuta, `f` reexecuta só as falhas, `c` cancela, `l` mostra os logs completos (stdout e stderr), `w` liga o modo watch e `a` alterna entre alvos e resultados.

Segurança:

- A primeira execução da sessão pede confirmação, porque testes rodam código do repositório e podem ter efeitos externos.
- Comandos são montados como vetores de argumentos, com `exec.CommandContext`, sem shell, em um grupo de processos próprio. O cancelamento e o timeout alcançam os processos filhos.
- Só uma execução por vez é permitida.
- O Node usa o binário local ou `npx --no-install`, que nunca baixa pacotes.
- Nada roda automaticamente. O modo watch precisa ser ligado explicitamente com `w`.

## Code Flow (`f`, `gitview flow`, aba no HTML)

Mostra o fluxo de chamadas real de funções Go, nas duas direções:
- **callers**: quem pode chamar a função;
- **callees**: o que ela pode chamar.

A análise é estática e feita com `go/packages`, `go/types` e SSA (`golang.org/x/tools/go/ssa`). Uma ligação entre funções nunca é deduzida pelo nome, pasta ou convenção de camadas.

```sh
gitview flow --function GetClientByID --direction both --depth 5
gitview flow --function GetClientByID --direction callers --depth 5
gitview flow --function GetClientByID --direction callees --depth 5
gitview flow --function 'client.(*ClientService).GetClientByID'   # nome qualificado
gitview flow --function service/client.go:84                      # arquivo:linha
gitview flow --find Client          # busca símbolos (autocomplete)
gitview flow --routes               # rotas HTTP e o que cada uma executa
gitview flow                        # funções modificadas no working tree
gitview flow --function X --json    # grafo em JSON
```

Para usar como `gv flow`, crie um alias: `ln -s "$(go env GOPATH)/bin/gitview" "$(go env GOPATH)/bin/gv"`.

Funcionamento:

- **Símbolos com o mesmo nome** nunca são escolhidos automaticamente.
  - Em um terminal, abre-se um seletor com pacote, receiver e arquivo:linha, que pode ser filtrado.
  - Com `--non-interactive` ou sem TTY, o comando sai com código 3 e lista os candidatos.
  - Um nome qualificado (`pkg.(*T).M`, `(*T).M`, `T.M`), o ID completo ou `arquivo:linha` desambigua.
- **Profundidade**: `--depth` (padrão 5) vale separadamente para callers e para callees. Nós no limite ficam marcados como truncados.
- **TUI (`f`)**: lista primeiro as funções modificadas e permite buscar em todo o módulo.
  - `Tab` alterna a direção e `+`/`-` mudam a profundidade.
  - `Enter` usa a função selecionada como raiz (Set as root), e `Backspace` volta à raiz anterior.
  - `g` abre a definição no diff, se ela foi modificada.
  - A análise roda em segundo plano, pode ser cancelada com `Esc` e não bloqueia o monitoramento do Git.
- **HTML**: com a opção "Code Flow" da exportação (HTML e JSON), o relatório embute o índice completo de chamadas do módulo, calculado no Go.
  - O navegador apenas navega esse índice: busca com autocomplete e seleção de homônimos, Callers / Both / Callees, profundidade de 1 a 5, zoom e pan, expandir e recolher ramos, caminho destacado, busca no grafo e filtro de bibliotecas externas.
  - Há também um painel com assinatura, pacote, arquivo:linha, chamadas, chamadores, código e explicação, além de "Set as root", "Open in diff", visão em lista acessível e navegação por teclado.
  - Funciona offline, sem servidor e sem CDN.
- **Cache**: a análise fica em cache por versão do código (caminho, tamanho e data de modificação dos `.go`, `go.mod` e `go.sum`) e é reaproveitada até algum desses arquivos mudar. As dependências são carregadas só como tipos (export data), sem analisar o código de terceiros.

Como cada chamada é classificada:

| Situação | Resultado |
|---|---|
| Chamada estática, ou valor de função conhecido no local | `confirmed` |
| Interface com uma única implementação no módulo | `likely` |
| Interface com várias implementações, ou função passada como argumento (callback, `errgroup.Go`) | `possible`, um destino por implementação |
| Valor de função em parâmetro, campo ou variável; interface sem implementação no módulo | `unresolved` |
| `go f()` e `defer f()` | arestas `go` e `defer` |
| Recursão e ciclos | detectados por componentes fortemente conexos (↻) |
| Fora do módulo (stdlib, dependências) | nós `external`, sem expansão |
| pgx, `database/sql`, sqlx: `Query`, `QueryRow`, `Exec`, `Begin`, `Commit`, `Rollback`, `SendBatch`, `CopyFrom`... | nó de banco com o SQL literal, ou marcado como **SQL dinâmico** |
| Interface do módulo implementada por `*pgxpool.Pool`, `*sql.DB` etc. | operação de banco `possible` |

**Rotas HTTP (net/http, Gin, Chi, Echo)**:
- Prefixos de `Group`/`Route` são resolvidos, inclusive quando o roteador é passado como parâmetro ou criado por uma função do módulo.
- Middlewares registrados com `Use` entram na cadeia, assim como handlers diretos, métodos, closures, factories que retornam closures, e listas montadas por helpers ou por `append(...)...`.
- Registros cujo caminho, método ou handler são calculados em tempo de execução aparecem como **parciais**.

**Limites da análise estática**:
- As arestas são caminhos possíveis, não execuções observadas. Nenhuma query é dada como executada.
- Chamadas por reflexão, por valores de função guardados em campos ou mapas, ou feitas por código fora do módulo (bibliotecas que chamam callbacks) não podem ser resolvidas, ou aparecem como `unresolved`/`possible`.
- Implementações de interface só são procuradas no módulo analisado. Arquivos `_test.go` não fazem parte do grafo.
- Pacotes que não compilam são ignorados e os demais continuam sendo analisados. Os erros aparecem na CLI e no relatório, e os resultados ficam parciais.
- O grafo é limitado por profundidade e por quantidade de nós (`--max-nodes`; no navegador, 320 nós por visão). Quando um limite é atingido, isso é sinalizado.

## Go Deep Inspector (`i`, `gitview inspect`, aba no HTML)

Estuda uma função Go em seis visões que compartilham o mesmo símbolo — escolhido com Find Function ou "Set as root" —, sem modelos de IA: cada resultado aponta para um símbolo, arquivo e linha, comando, perfil ou evento de execução.

| Visão | Base | Natureza do resultado |
|---|---|---|
| **Code Flow** | call graph (SSA) | estático: caminhos possíveis |
| **Data Flow** | cadeias def-use do SSA | estático: fluxos possíveis |
| **Execution Trace** | `runtime/trace` (`go test -trace`) | **observado** em uma execução |
| **Branch Explorer** | `golang.org/x/tools/go/cfg` (+ cobertura) | estático (cobertura: observada) |
| **Performance** | pprof, `go test -bench`, `-gcflags=-m=2` | perfis **estimados** (amostragem); benchmarks **medidos**; diagnósticos literais do compilador |
| **SQL Explorer** | chamadas pgx, pgxpool, database/sql, sqlx (+ EXPLAIN opcional) | estático; planos do PostgreSQL são **estimativas** (ou observações, com ANALYZE) |

A identidade do símbolo é o nome qualificado do SSA (pacote, receiver e nome: `(*example.com/app/service.ClientService).GetClientByID`), acompanhada de assinatura, arquivo, linhas, módulo e versão do código analisado. Homônimos nunca são escolhidos automaticamente. Quando faltam dados, a visão diz isso — `No runtime trace available`, `No profiling data available`, `SQL query not statically resolved`, `No matching symbol found` — em vez de mostrar dados simulados.

### Na TUI

`i` abre o inspector (ou `f` e depois `1`–`6`). Abas: `1` Code Flow · `2` Data Flow · `3` Execution Trace · `4` Branch Explorer · `5` Performance · `6` SQL Explorer; `[` e `]` circulam. A linha selecionada mostra o trecho de código responsável logo abaixo; `g` abre a linha no diff, se ela foi modificada; `/` busca outra função; `I` importa um arquivo (cobertura, trace, perfil pprof ou saída de benchmark); `R` recalcula.

- **Data Flow**: `v` escolhe a variável (parâmetros, locais e resultados nomeados), `o` origem, `d` destino, `b` ambos, `+`/`-` profundidade entre funções, `h` oculta valores intermediários do SSA.
- **Branch Explorer**: `p` lista os caminhos até cada saída; `c` executa os testes do pacote com cobertura (pede confirmação).
- **Performance**: `b` executa os benchmarks do pacote com `-benchmem` e perfis de CPU e memória; `e` compila com `-gcflags=-m=2`; `m` alterna CPU/memória; `F` mostra o flame graph em texto. A partir da segunda execução de benchmarks, a comparação antes/depois aparece.
- **Execution Trace**: `t` executa os testes do pacote gravando um trace; `T` faz o mesmo medindo a função selecionada; `+`/`-`/`←`/`→` aproximam e deslocam a linha do tempo, `0` volta ao trace inteiro, `a` inclui goroutines do runtime.
- **SQL Explorer**: `r` alterna entre as operações da função e as alcançáveis por chamadas; `x` pede o plano (EXPLAIN) da operação selecionada; `X`, EXPLAIN ANALYZE de instruções somente leitura.

Tudo que executa código mostra o comando completo e espera `Enter`; `Esc` cancela antes ou durante a execução. As análises estáticas rodam em segundo plano e ficam em cache por versão do código.

### Na linha de comando

```sh
gitview inspect --function GetClientByID                       # resumo das seis visões
gitview inspect dataflow --function GetClient --variable clientID
gitview inspect dataflow --function GetClient --variable clientID --direction origin --depth 4 --hide-intermediate
gitview inspect dataflow --function FindByID --at repository/pg.go:22:76   # expressão em arquivo:linha:coluna
gitview inspect branches --function GetClientByID --paths
gitview inspect branches --function GetClientByID --cover cover.out        # ou --run-coverage
gitview inspect branches --function GetClientByID --dot | dot -Tsvg > cfg.svg
gitview inspect sql --function FindByID
gitview inspect sql --function GetClientByID --reachable
gitview inspect sql --all --json
gitview inspect sql --function FindByID --explain --dsn postgres://localhost/dev
gitview inspect performance --cpu cpu.prof --function Busy --flame
gitview inspect performance --memory mem.prof --sample inuse_space
gitview inspect performance --bench . --package ./perf --count 6     # executa (pede confirmação)
gitview inspect performance --escape ./perf                          # compila com -m=2
gitview inspect performance --compare antigo.txt,novo.txt
gitview inspect trace --import trace.out --function Square
gitview inspect trace --test TestPipeline --package ./traced --instrument Square
gitview inspect report --function GetClientByID,FindByID --cpu cpu.prof --trace trace.out -o inspect.html
```

Use `gv inspect …` com o mesmo alias do Code Flow. Em um terminal, homônimos abrem o seletor; com `--non-interactive` ou sem TTY, o comando sai com código 3 e lista os candidatos. Outros códigos: 1 erro, 2 uso, 4 execução não confirmada. Toda execução (testes, benchmarks, builds, EXPLAIN) pede confirmação; sem TTY, só acontece com `--yes`. Todas as visões aceitam `--json`.

### Data Flow

Selecione uma variável, um parâmetro, um resultado (`--return 0`) ou uma expressão (`--at`), e veja de onde o valor vem (**Trace Origin**), para onde pode ir (**Trace Destination**) ou ambos.

- O rastreamento segue atribuições, conversões, φ (valores que dependem do caminho), campos de structs e memória local (ponteiros), argumentos ↔ parâmetros e retornos ↔ resultados entre funções, closures e interfaces.
  - Os retornos voltam ao local de chamada de onde se entrou (contexto de profundidade 1).
  - A profundidade (`--depth`) conta fronteiras de função atravessadas. Ao atingir um limite, o resultado fica **parcial** e diz por quê.
- **Fontes** reconhecidas pela API:
  - parâmetros HTTP: `chi.URLParam`, `r.PathValue`, `r.FormValue`, `r.URL.Query().Get`, `r.Header.Get`, Gin, Echo e corpo JSON decodificado de `r.Body`;
  - linhas do banco escritas por `Scan`;
  - variáveis de ambiente;
  - constantes.
- **Destinos** destacados: texto SQL e parâmetros `$N` de operações de banco, chamadas externas e retornos sem chamadores no módulo.
- **Validações**: condições sobre o valor, ou sobre um valor derivado dele, que decidem um desvio (`id <= 0`, `!exists(id)`, `if err != nil` sobre o erro da conversão).
- Cada aresta tem confiança:
  - `exact`: def-use do SSA, binding de parâmetro, retorno;
  - `derived`: o valor é calculado a partir da origem (conversão, aritmética, função de biblioteca);
  - `likely`/`possible`: interface com uma ou várias implementações, ou memória que pode ter alias;
  - `unresolved`: a análise não consegue seguir o valor.
  Nada é dado como `exact` sem que a relação seja demonstrada.

### Execution Trace

Captura (`go test -trace`, com timeout e cancelamento) ou importa traces do formato Go 1.11+ e mostra somente eventos registrados:

- estados de cada goroutine (executando, pronta, esperando com o motivo, syscall), com pilhas;
- fases de GC e stop-the-world, tarefas, regiões e logs de `runtime/trace`;
- as esperas mais longas, filtros por goroutine, tarefa e intervalo, e a correlação com a função selecionada: goroutines iniciadas nela, bloqueios com ela na pilha e regiões que a medem.

O trace **não** registra cada chamada de função. Duração por função só existe em regiões: as do próprio programa, ou as inseridas por `--instrument`/`T`.

- A instrumentação insere `defer trace.StartRegion(...).End()` no início da função em uma **cópia** do arquivo, passada ao compilador com `go test -overlay`. Nenhum arquivo do projeto é alterado, e os números de linha são preservados.
- Essas regiões aparecem como `gitview: <símbolo>` e incluem o custo da própria instrumentação.

### Branch Explorer

O CFG da função, com decisões (`if`, `for`, `range`, `switch`, `type switch`, `select`), `break`, `continue`, `goto` e `fallthrough` nomeados, retornos (inclusive o implícito), `panic`/`os.Exit`/`log.Fatal`, `defer`, laços (arestas de volta) e blocos inalcançáveis.

- `select` sem `default` não tem saída direta: ele espera um caso.
- Verificações de erro (`err != nil`, `errors.Is`/`As`) e retornos que devolvem erro são destacados.
- Métricas: blocos, arestas, decisões, complexidade ciclomática (decisões + 1), retornos, laços e caminhos (cada laço repetido no máximo uma vez).
- O CFG é estrutural: um caminho listado é permitido pelo código, não observado, e pode ser inviável.
- Com um perfil de cobertura (`--cover`, `--run-coverage`, `c` ou `I` na TUI), cada bloco aparece como coberto, não coberto, parcial ou desconhecido.
  - A cobertura do Go conta blocos de instruções: não prova quais arestas foram tomadas.
  - Um perfil mais antigo que o arquivo-fonte é ignorado (cobertura desconhecida).

### Performance

- **Perfis** pprof (CPU, alocações, memória em uso, bloqueio, mutex) importados ou gerados pelos benchmarks: ranking com flat/cum, linhas quentes, call graph do perfil, flame graph e a função selecionada (mapeada pelo mesmo símbolo, por arquivo e linha).
  - Em memória, `alloc_*` são alocações acumuladas e `inuse_*` a memória em uso: são métricas diferentes, e ambas são estimativas por amostragem.
- **Benchmarks**: `go test -run=^$ -bench … -benchmem -count N -cpuprofile -memprofile`, com o binário de teste e os perfis gravados no diretório de cache do usuário, nunca no repositório.
  - A tabela traz ns/op, B/op, allocs/op e métricas próprias.
  - A comparação antes/depois usa o `benchstat` instalado ou, sem ele, o mesmo teste (Mann–Whitney U, α = 0,05), e avisa quando as condições (CPU, GOOS, pacote…) diferem.
- **Diagnósticos do compilador**: `go build -gcflags=-m=2 -o /dev/null` (compila sem executar), agrupados por função, com as explicações `-m=2` literais, sem interpretação adicional.

### SQL Explorer

Lista as operações de banco da função (`--reachable`: também as das funções chamadas, com o caminho e a confiança):

- `Query`, `QueryRow`, `Exec`, `Prepare`, `Begin`/`BeginTx`, `Commit`, `Rollback`, `SendBatch`, `Batch.Queue`, `CopyFrom`;
- o SQL como literal, constante nomeada (com sua posição), uma de várias alternativas literais, ou modelo parcial (concatenação, `fmt.Sprintf`), em que as partes calculadas aparecem como `⟨expr⟩` — nunca completadas por suposição;
- parâmetros `$N` com a expressão Go e a origem encontrada pelo Data Flow;
- destinos de `Scan`, transações (início, operações, commit, rollback adiado) e divergência entre placeholders e argumentos.

Os IDs das operações são os mesmos nós do Code Flow.

**PostgreSQL EXPLAIN (opcional)** — componente separado, que só roda com conexão explícita:

- Conexão: `--dsn` ou `GITVIEW_PG_DSN`. Nunca `DATABASE_URL`, `.env` ou configuração do projeto. Como no `psql`, a biblioteca pode completar campos ausentes com variáveis `PG*` e `~/.pgpass`.
- Só servidores locais (loopback ou socket Unix), salvo `--allow-remote`.
- Uma única instrução `SELECT`/`INSERT`/`UPDATE`/`DELETE`/`MERGE`.
- `EXPLAIN` roda numa transação `READ ONLY`, sempre desfeita, com `statement_timeout` e `lock_timeout`.
  - Consultas com `$1` sem valores usam `GENERIC_PLAN` (PostgreSQL 16+).
- `EXPLAIN ANALYZE` **executa** a instrução: exige `--analyze`, confirmação e, se alterar dados, `--allow-write`. Mesmo assim roda numa transação desfeita, e efeitos fora da transação (sequências, efeitos externos de triggers) não são desfeitos.
- Credenciais e valores de parâmetros nunca vão para a saída nem para relatórios (os valores impressos pelo PostgreSQL no plano voltam a ser `$N`).

### Relatório HTML

A opção **Go Deep Inspector** do `x` (padrão ligada) e `gitview inspect report` embutem as seis visões para as funções modificadas e a inspecionada, além dos dados de execução importados ou capturados na sessão.

- O relatório tem seletor de função, abas (com link `#inspect:<símbolo>:<aba>` compartilhado com o Code Flow), grafos de Data Flow e Branch Explorer com zoom e pan e painel de detalhes com o código, linha do tempo do trace com filtros, flame graph, tabelas filtráveis e planos de execução.
- É um arquivo único que funciona offline. A Content-Security-Policy por hash continua bloqueando rede e scripts injetados, e todo texto do repositório é inserido como texto.
- Caminhos fora do repositório aparecem como `$GOROOT`, `$GOMODCACHE` ou `~`.
- Sem trace ou perfil, o relatório diz isso.
- `gitview inspect report` não inclui os diffs, salvo com `--diff`.

### Limites

- Análises estáticas não executam código. Reflexão, valores de função guardados em campos ou mapas, canais entre goroutines, `unsafe` e código fora do módulo não são seguidos; aparecem como `unresolved` ou `possible`.
- A análise de memória é intraprocedural: escritas e leituras por ponteiros recebidos como parâmetro aparecem como pontos não resolvidos.
- Implementações de interface só são procuradas no módulo. Arquivos `_test.go` não fazem parte das análises estáticas.
- Funções de biblioteca são opacas: o resultado é considerado derivado dos argumentos de dados (literais de configuração, como a base de `ParseInt`, não contam como origem).
- A captura de trace e os benchmarks executam o pacote inteiro escolhido. A instrumentação temporária vale para funções de nível superior (não closures).
- Perfis e traces são relacionados ao código analisado agora. Se o código mudou depois da captura, as linhas podem não corresponder.
- O cache das análises é por módulo e por versão (caminho, tamanho e data dos `.go`, `go.mod`, `go.sum`): uma alteração em qualquer arquivo Go do módulo recarrega a análise do módulo inteiro.

## Runtime Performance Inspector (`m`, `gitview runtime`)

Mede o comportamento de uma aplicação Go **já em execução**, sem benchmarks: escolha o processo pelo PID, inicie a captura, execute a rota pelo Postman ou curl e encerre. O GitView registra o processo, correlaciona as requisições quando há instrumentação HTTP e relaciona o que observou ao código Go do repositório.

### Fontes de dados

| Fonte | Exige | O que fornece | Natureza |
|---|---|---|---|
| `/proc/<pid>` | nada (mesmo usuário; de outros usuários, só parte) | CPU (ns por thread via schedstat), RSS, threads, trocas de contexto, E/S, descritores, portas em escuta, versão Go e módulo do binário | **medido**, do processo inteiro |
| Proxy local | nada: o cliente envia ao proxy (`127.0.0.1`) | método, caminho (sem query string), status, tempo total e até os cabeçalhos, bytes, `X-GitView-Trace-ID` | **medido** no lado do cliente |
| Collector do GitView | [`collector`](collector/README.md) na aplicação | padrão da rota, tempo no handler, SQL com a linha que o executou, CPU por requisição (pprof labels), task de trace por requisição, `runtime/metrics` | **medido**; CPU por requisição **amostrada** |
| `net/http/pprof` | a aplicação já servi-lo | perfis de CPU, heap/alocações, goroutines, mutex, bloqueio e execution trace | **amostrado** (trace: **observado**) |
| OpenTelemetry | exporter OTLP/HTTP apontado para `127.0.0.1:4318` (`--otlp`) | spans HTTP e de banco | **medido** pela aplicação |

Métricas do processo valem para o processo inteiro e são rotuladas assim. Números por requisição só aparecem quando há como atribuí-los: tempo e SQL (medidos pelo proxy/collector; sem collector nem OpenTelemetry, o SQL é "não observado", não zero), CPU por requisição (amostras com o label da requisição, só para requisições inteiramente dentro de uma janela do perfil de CPU) e uma estimativa de CPU acima da linha de base para requisições isoladas e mais longas que dois intervalos de amostragem. O que não pode ser coletado aparece como indisponível, com o motivo — por exemplo, bytes de rede por processo, que o Linux não expõe fora de um network namespace próprio.

### Captura

1. **Linha de base** (padrão 2 s): o processo é medido antes das requisições.
2. **Fase ativa**: até `Enter`/`Ctrl+C`, `--duration` ou o limite (10 min). Pode ser pausada (`Espaço` na TUI): o intervalo fica fora da análise.
3. **Pós-captura** (padrão 2 s): o processo continua medido; snapshots finais dos perfis são feitos fora da janela medida.

Os tempos usam o relógio monotônico. O processo é amostrado a cada 250 ms (50 ms–5 s). Se ele terminar ou o PID for reaproveitado, a captura termina, é salva e diz o que aconteceu (e se o mesmo executável voltou com outro PID).

O relatório traz: CPU e RSS ao longo do tempo com as fases e as requisições marcadas; estatísticas por fase; requisições por rota (p50/p90/p99 por *nearest rank*, status, bytes, concorrência); a rota relacionada à função que a registra e ao handler; SQL por rota com a operação correspondente do SQL Explorer (pela linha de chamada, ou pelo texto); caminho crítico e flame graph das amostras de CPU de cada rota; alocações, contenção, bloqueios e goroutines; métricas do runtime; o que o execution trace mostra de cada requisição (executando, pronta sem P, esperando por motivo, GC simultâneo); e as limitações da captura.

### Comparação

`gitview runtime compare A B` (ou `c` na TUI) compara duas capturas da mesma rota: latência com teste de Mann–Whitney, CPU por requisição, SQL por requisição (instruções novas e removidas), métricas do processo e funções cuja participação na CPU mudou. Antes, verifica se as condições são comparáveis — mesmo programa, mesmo tamanho de máquina, mesma instrumentação, linha de base parecida e tranquila — e marca o resultado como **inconclusivo** quando não são: linha de base não medida, menos de 5 requisições por lado, concorrência não medida ou diferente (máxima e média, e taxa de requisições quando elas se sobrepõem) ou latências medidas de formas diferentes. Uma regressão só é apontada com variação da mediana ≥ 5% e p < 0,05 após a correção de Holm para várias rotas.

### Na TUI

`m` abre a tela: `↑↓`/`Enter` escolhe um processo Go deste usuário (`p` digita um PID), `1`–`7` ligam proxy, collector e perfis, `d` define a duração, `f` o intervalo de amostragem, `t` a porta da aplicação atrás do proxy, e `Enter` inicia — com perfis, a lista exata de requisições ao processo aparece antes e só segue com `Enter`. Durante a captura: CPU e RSS em tempo real, endereço do proxy e requisições detectadas; `Enter` encerra, `Espaço` pausa, `x` cancela sem salvar. O indicador `● captura` fica na barra superior nas outras telas. No relatório, `Enter` sobre um handler, uma função do caminho crítico ou uma instrução SQL abre a função no Go Deep Inspector (com o perfil de CPU da captura na aba Performance) e `Esc` volta; `r` filtra uma rota, `e` exporta HTML, `c` compara, `h` abre o histórico (`D` remove).

### Na linha de comando

```sh
gitview runtime ps                                   # processos Go deste usuário
gitview runtime inspect 12345                        # o que pode ser medido nesse processo
gitview runtime capture 12345                        # proxy local + collector; Enter encerra
gitview runtime capture 12345 --duration 30s --label "antes do índice"
gitview runtime capture 12345 --profile cpu,heap,trace --yes -o captura.html
gitview runtime capture 12345 --otlp 127.0.0.1:4318 --no-proxy
gitview runtime history
gitview runtime show 20261015-1125 -o relatorio.html  # prefixo do ID basta
gitview runtime compare <id-antes> <id-depois> -o comparacao.html
gitview runtime delete <id>
```

`--json` em todos os comandos. Códigos de saída: 0, 1 erro, 2 uso, 4 profiling não confirmado, 130 cancelado. O relatório é relacionado ao módulo do repositório que gerou o binário (mesmo caminho de módulo); `--repo` escolhe o repositório e `--no-source` dispensa a análise do código.

### Segurança e limites

- Tudo é local: o proxy, o receptor OTLP e o collector só aceitam endereços de loopback, e o GitView só se conecta a portas do próprio processo. O collector exige um token, gravado com permissão `0600` em `$XDG_RUNTIME_DIR/gitview/collectors/<pid>.json`, recusa clientes remotos e requisições com cabeçalhos de proxy, e fica inerte até uma captura armá-lo (com prazo para se desarmar sozinho).
- Um `net/http/pprof` só é usado depois de conferir que a linha de comando que ele informa é a do processo (a comparação é feita em memória; linhas de comando nunca são gravadas). Se ele aceitar conexões externas, o GitView avisa. O GitView só abre conexões para endereços de loopback e não segue redirecionamentos. O anúncio do collector só é aceito se for um arquivo comum, do próprio usuário e sem permissão para outros, com endereço de loopback de uma porta do processo.
- Processo em outro network namespace (contêiner): as portas dele não são acessíveis em `127.0.0.1` daqui, então o GitView não as sonda; use `--target` com um endereço que alcance a aplicação.
- O collector é armado com uma concessão (lease): outra sessão do GitView não consegue desarmá-lo nem mudar taxas de perfil no meio de uma captura.
- Não exige root. Processos de outros usuários podem ser inspecionados com menos métricas, e o motivo de cada uma indisponível é mostrado.
- Nunca são registrados cabeçalhos, cookies, `Authorization`, query strings, corpos, parâmetros de SQL ou literais de SQL (também os de MySQL: `\'`, `"…"`, comentários `#`). Do caminho, só ficam segmentos que são palavras (`clients`, `v2`, `oauth2`); os demais viram `{n}`, `{uuid}`, `{hex}` ou `{value}`.
- Cada requisição tem um ID próprio, mesmo quando o cliente repete o `traceparent` ou o `X-GitView-Trace-ID`: o trace W3C identifica um trace, não uma requisição. Spans OpenTelemetry sem `process.pid` são ignorados (podem ser de outro serviço), salvo com `--otlp-any-process`; o receptor espera até 6 s após a fase ativa pelos últimos lotes.
- Profiling só com confirmação explícita (`--yes` sem terminal). CPU e trace ao mesmo tempo, perfis de mutex/bloqueio desligados na aplicação e heap sem GC geram avisos. Um perfil de CPU já em uso no processo é reportado como conflito, não sobrescrito.
- Histórico em `~/.cache/gitview/<repo>/runtime/` (ou `GITVIEW_CACHE_DIR`): no máximo 30 capturas e 512 MiB, removendo as mais antigas; cada perfil ou trace pode ocupar até um quarto desse limite, e uma captura que o exceda perde primeiro o trace (avisado no relatório). `GITVIEW_RUNTIME=off` desativa o inspetor; `GITVIEW_COLLECTOR=off` desativa o collector na aplicação.
- Linux apenas (`/proc`). Para relacionar linhas, o código do repositório deve ser o que gerou o binário: arquivos alterados depois que o processo iniciou são apontados.

## Study Mode (aba Study, `gitview study`)

Uma camada de estudo sobre as análises que o GitView já faz (call graph, CFG, Data Flow, SQL, cobertura, traces, perfis e diff), para entender uma alteração por conta própria. Não usa IA, LLM, embeddings nem serviços externos, e não gera explicações: tudo é derivado de forma determinística (a mesma entrada produz o mesmo modelo, identificado por uma impressão digital). O que não pode ser determinado aparece como `Unknown`, "Not detected" ou "no data". Cada item mostra de onde vem, com os selos `STATIC`, `RUNTIME`, `COVERAGE`, `SQL`, `GIT` e `HEURISTIC`.

```sh
gitview study                                   # gitview-study.html, com a aba Study e os diffs
gitview study --cover cover.out --trace trace.out --cpu cpu.out --memory mem.out \
              --bench-file bench.txt --escape-log build.log -o estudo.html
gitview inspect report --study                  # o mesmo, a partir do relatório do inspector
```

Na TUI, a exportação HTML (`x`) tem a opção **Study** (padrão ligada). Os arquivos opcionais são apenas lidos; nada é executado para gerar a aba.

- **Start Here**: ordem de leitura reproduzível, com a rota ou o ponto de entrada, as funções alteradas e as dependências diretas, persistência, SQL alterado, testes e demais arquivos, cada passo com o motivo.
- **Architecture**: camadas (rota, middleware, handler, serviço, repositório, banco, API externa…) por papel estrutural comprovado no código (`STATIC`) ou, na falta dele, pelo nome do diretório (`HEURISTIC`). Inclui as cadeias rota → handler → serviço → repositório → banco e o grafo de chamadas entre camadas.
- **Change Journey**: arquivos alterados agrupados por camada, com as funções alteradas e quem as chama ou é chamado por elas.
- **Data Journeys**: caminhos de valores do Data Flow, de SOURCE a SINK, com TRANSFORMATION e DECISION no meio.
- **Decisions**: cada condição do CFG com o que acontece em cada saída (primeira instrução, retorno, erro, atribuições), sem interpretar o motivo da regra.
- **Tests**: quais testes alcançam cada função (lidos do código de teste e do call graph) e, separadamente, quais saídas o perfil de cobertura mostra executadas, com o caminho até cada ramo não observado.
- **Predict Execution**: monte a ordem em que espera que as funções rodem e compare com o execution trace (Missed, Unexpected, ordem correta e incorreta) ou com a ordem das chamadas no código.
- **SQL**: cada query com tabelas, JOINs, filtros, `ORDER BY`, `GROUP BY`, `LIMIT`, parâmetros, quem chama e os índices declarados nos `.sql` do projeto. Não diz se um índice é bom ou ruim; só um plano EXPLAIN mostra se é usado.
- **Performance**: o que foi medido (perfis, benchmarks, regiões do trace) e o que é estático (loops, decisões, chamadas ao banco, diagnósticos do compilador), em seções separadas.
- **Explain It Yourself**: checklist por função com botões Reveal que mostram os fatos encontrados e um campo para a sua explicação.
- **Quiz** e **Challenges**: perguntas de modelos fixos (assinatura, chamada, CFG, tabela, origem e destino de dados, handler da rota, camada, ramo para valores dados, ordem no trace). Cada uma tem uma resposta certa, alternativas comprovadamente erradas e a evidência; sem alternativas confiáveis, a pergunta não é criada.
- **Progress**: o que foi aberto, marcado ou respondido, guardado no `localStorage` do navegador por relatório, com **Reset Progress**. Conta atividade, não mede conhecimento.

Limites:

- Que um teste alcança uma função é estático; o perfil de cobertura é do conjunto de testes, então não diz qual teste executou qual ramo.
- O trace só registra pilhas em eventos do escalonador e em regiões: uma função ausente pode ter rodado.
- Índices são lidos dos `.sql` sem considerar a ordem das migrações nem o banco real.
- Desafios de ramo só usam condições com operandos inteiros ou booleanos (tipos verificados pelo type checker) e os operadores `> < >= <= == != && || !`.

## Repository Graph (`a`, `gitview graph`)

Transforma o repositório Go inteiro em um grafo de conhecimento estrutural, navegável num HTML offline: módulos, pacotes, arquivos, structs, interfaces, funções, métodos, closures, rotas HTTP e operações SQL, ligados pelas relações encontradas no código. Nenhuma relação é deduzida pelo nome de uma função ou arquivo, nada do repositório é executado, e nenhum arquivo dele é modificado.

```sh
gitview graph export                                # gitview-graph.html, todos os módulos do repositório
gitview graph export --output architecture.html
gitview graph export --format json --output graph.json
gitview graph export --format json --level packages # dataset filtrado
gitview graph export --level functions              # visão inicial do HTML
gitview graph export --module backend               # só um módulo
gitview graph stats                                 # métricas (também --json)
```

O arquivo nunca é sobrescrito sem `--overwrite`. Na TUI, `a` abre o painel de controle: escolha o escopo (repositório inteiro ou um módulo), `Enter` analisa em segundo plano (cancelável com `Esc`, sem bloquear o monitoramento do Git), `x` exporta o HTML e `J` o JSON (arquivos novos com data e hora, no diretório de exportação), e `o` abre o último HTML no navegador. Milhares de nós não são desenhados no terminal: a TUI controla, o HTML visualiza.

### De onde vem cada relação

O grafo é uma camada de transformação sobre os analisadores existentes (**Code Flow → Repository Graph Builder → dataset → visualização**). Não há um segundo motor de análise: o grafo reaproveita a análise do Code Flow, e o cache dela, que só recarrega um módulo quando os seus `.go`, `go.mod` ou `go.sum` mudam.

| Relação | Origem | Confiança |
|---|---|---|
| `contains` (módulo → pacote → arquivo → declaração, função → closure) | go/packages, go/types, SSA | confirmed |
| `imports` | declarações `import` (evidência: arquivo:linha; peso: arquivos que importam) | confirmed |
| `calls`, `go`, `defer` | índice do Code Flow (SSA) | confirmed, likely, possible ou unresolved, como no Code Flow |
| `handles` (rota → handler/middleware), `queries` (função → operação SQL) | reconhecedores do Code Flow (net/http, gin, chi, echo; pgx, database/sql, sqlx) | idem |
| `implements` | `types.Implements` para cada tipo concreto e cada interface não vazia do mesmo módulo (method set do valor ou do ponteiro) | confirmed |
| `embeds`, `references` (campos e assinaturas que usam tipos do módulo), `method` | declarações go/types | confirmed |

- Toda aresta traz evidência (`go/ssa`, `go/types` ou `go/packages`, com arquivo e linha).
- Várias chamadas entre as mesmas funções viram uma aresta com `weight` e a lista de locais.
- Uma chamada sem destino determinável aponta para um nó `unresolved` explícito, nunca para um destino inventado. A completude da análise (`complete`/`partial` e os motivos) faz parte do dataset.

### Dataset (`gitview.graph/v1`)

- **IDs estáveis** derivados só do símbolo:
  - `package:<import path>`, `file:<caminho>`, `type:<pkg>.<Nome>` e `func:<nome SSA>`. Por exemplo, `func:(*example.com/app/users.Service).GetUser`. Go não tem sobrecarga, então pacote, receiver e nome identificam a função, e as closures levam o sufixo `.func1`.
  - Rotas, SQL, funções externas e chamadas não resolvidas mantêm os IDs do Code Flow.
  - Duas execuções sobre o mesmo código produzem o mesmo arquivo, exceto pelos tempos e pela data.
- Cada nó traz: tipo, pacote, arquivo:linha, assinatura, comunidade, grau (de entrada e de saída), posição, `metadata`, e `annotations`, reservado para análises futuras (findings de auditoria, por exemplo).
- `--level` filtra o JSON:
  - `packages`: pacotes e imports;
  - `files`: arquivos, com arestas `depends` **derivadas** das relações entre as declarações, marcadas `derived`;
  - `functions`: funções e chamadas;
  - `types`: tipos e suas relações;
  - `full`: tudo (padrão).

### Comunidades e layout

- **Comunidades** são detectadas com o algoritmo de Louvain (modularidade, resolução 1), de forma determinística:
  - os nós são visitados em ordem de ID, e um nó só muda de comunidade quando o ganho é estritamente maior; os empates mantêm a comunidade atual e depois favorecem o menor índice;
  - roda em dois níveis: pacotes (imports mais relações entre símbolos de pacotes diferentes) e símbolos (chamadas, relações de tipo e closures);
  - o rótulo de cada comunidade é o nome do membro mais conectado. São grupos estruturais, não camadas arquiteturais.
- **Layout** calculado no Go, também determinístico:
  - Fruchterman–Reingold para os pacotes;
  - os membros de cada pacote ficam num disco em volta dele;
  - assim, um símbolo ocupa o mesmo lugar no Overview e no Module Explorer.

### O HTML

Um único arquivo, que funciona por `file://`, sem servidor, CDN ou requisições de rede.

- **Renderização**: Sigma.js 3.0.2 com Graphology 0.26.0 (WebGL, MIT), embutidos em `internal/graph/export/assets` com as licenças e o hash dos arquivos originais.
- **Segurança**: a Content-Security-Policy (`default-src 'none'`) permite só os três scripts da página, fixados por SHA-256. Os dados ficam num bloco JSON inerte, e o texto do repositório entra na página com `textContent`.

Três escalas sobre o mesmo dataset, sem nova análise ao trocar de escala:

1. **Repository Overview**: pacotes e imports agrupados por comunidade. O seletor de nível alterna para File, Function, Type ou Full Repository Graph.
2. **Module Explorer** (duplo clique num pacote): os arquivos, tipos e funções do pacote, nas mesmas posições do Overview. Os pacotes conectados ficam num anel externo, com as relações agregadas por símbolo. Escolher um arquivo restringe a visão às declarações dele.
3. **Symbol Explorer** (duplo clique numa função ou tipo): chamadores à esquerda e chamados à direita, por profundidade (0 a 6 em cada direção), com setas e confiança. Para tipos, mostra implementações, embeds, referências e métodos.

Interações:

- **Navegação**: breadcrumbs Repository › pacote › arquivo › símbolo, e `Backspace` sobe um nível.
- **Busca** (`/`) por função, método, tipo, pacote ou arquivo, que abre o nó na escala certa e dá zoom nele.
- **Seleção**: painel com tipo, pacote, arquivo:linha, assinatura, comunidade, conexões, chamadores e chamados (com confiança e local da chamada) e dependências.
- **Foco**: só o nó selecionado e seus vizinhos, até a profundidade escolhida.
- **Caminho entre dois nós**: "Path from here" e depois clicar no destino. Procura primeiro um caminho dirigido e, se não houver, ignora a direção, avisando no painel.
- **Filtros** por tipo de nó e de relação, ocultar dependências externas, closures, rótulos e setas.
- **Comunidades**: mostrar/ocultar e destacar com as conexões externas.
- **Estrutura**: nós mais conectados, nós isolados e componentes conexos da visão.
- **Deep links** (`#package=…`, `#symbol=…`, `#level=…`) que preservam a visão ao recarregar.

Limites:

- **Limites configuráveis** (`--max-nodes`, `--max-edges` e o menu Filters): acima deles, a visão mantém os nós mais conectados e as relações mais fortes, e avisa quantos ficaram de fora. O dataset embutido continua completo.
- **Análise estática**: uma aresta é uma relação que pode acontecer, não uma execução observada.
  - `implements` só considera interfaces do mesmo módulo, já que cada módulo é verificado separadamente.
  - Referências a tipos cobrem campos e assinaturas, não o corpo das funções.
- **Tamanho**: repositórios com dezenas de milhares de arestas geram arquivos de alguns MB (o pgx, com 40 mil linhas, gera 5,4 MB).
- **WebGL**: sem ele, a página avisa, e a busca, as listas e os detalhes continuam funcionando.

## Architecture Journey: HTTP Explorer (`gitview graph routes|journey`, aba HTTP do Repository Graph)

Documenta e explora a API Go por análise estática. Para cada rota, mostra:

- onde ela foi registrada;
- os middlewares (globais, de grupo e da rota) e o handler;
- os services, repositories e operações SQL que a cadeia pode alcançar;
- os parâmetros, o corpo e as respostas que o código lê e escreve;
- um exemplo de requisição em cURL.

Nada é executado: o servidor da aplicação não é iniciado e nenhuma requisição é enviada.

```sh
gitview graph routes                                  # catálogo por prefixo
gitview graph routes --format json
gitview graph journey --route "POST /api/clients" --depth 4
gitview graph journey --route "route:POST /api/clients@routes/clients.go:42"   # registro exato
gitview graph export --output architecture.html       # HTML com a aba HTTP
gitview graph export --format openapi --output openapi.json
gitview graph export --format postman --output collection.json
gitview graph export --include-http=false             # sem o catálogo
```

### Rotas

A descoberta é a do Code Flow (gin, chi, echo, `net/http` e padrões `"GET /x/{id}"` do `ServeMux`), com o que o grafo precisava:

- **Prefixos:** grupos e rotas aninhadas, routers recebidos como parâmetro (inclusive interfaces como `gin.IRoutes`), `chi.Route` e `chi.Mount`. O sub-router montado herda o prefixo e os middlewares do pai.
- **Ordem dos middlewares:** cada middleware traz o escopo (global, group, router, route) e o local da chamada que o adicionou. No gin, um `Use` que roda depois do registro da rota (ou da criação do grupo) não é aplicado, como no gin real. Quando a ordem depende do caminho, o middleware é marcado `possible`.
- **Handlers indiretos:** middlewares passados em `Group("/x", mw)` e `chi.With(mw)`, handlers montados por funções auxiliares e por closures que capturam variáveis (`with(h)` → `[]HandlerFunc{user, h}`).
- **Caminhos dinâmicos:** um caminho calculado em tempo de execução mantém o prefixo constante (`/kinds/{dynamic}`) e é marcado como parcial; nunca vira uma rota inventada.

O ID de uma rota inclui o local do registro: é o mesmo ID do nó do grafo.

### Contratos

O analisador interpreta o SSA da cadeia de cada rota. Ele segue os helpers que recebem valores HTTP (`*gin.Context`, `http.ResponseWriter`, `*http.Request`, `echo.Context`), com os argumentos de cada chamada. Assim, um status passado a `httpx.JSON(w, status, v)`, ou guardado no campo de um `*Error` criado por `NewError(http.StatusNotFound, …)` numa variável de pacote, é resolvido até a constante. O analisador não entra nos services.

| Contrato | Origem reconhecida |
|---|---|
| Path | `c.Param`, `r.PathValue`, `chi.URLParam`, `ShouldBindUri` (tags `uri`) e o próprio padrão da rota |
| Query, header, cookie, form | `c.Query`/`DefaultQuery`/`GetQuery`, `r.URL.Query().Get`, `r.FormValue`, `c.GetHeader`, `r.Header.Get`, `c.Cookie`/`r.Cookie`, `c.PostForm`, `c.FormFile`, `ShouldBindQuery`/`ShouldBindHeader` |
| Corpo | `ShouldBindJSON`/`BindJSON`/`ShouldBind`/`ShouldBindWith`, `json.NewDecoder(r.Body).Decode` (com `DisallowUnknownFields`), `io.ReadAll(r.Body)` + `json.Unmarshal`, echo `Bind` |
| Respostas | `c.JSON` e variantes, `AbortWithStatusJSON`, `c.Status`, `c.String`, `c.Data`, `c.Redirect`, `http.Error`, `http.NotFound`, `WriteHeader` + `json.Encoder.Encode`/`Write`, echo `JSON`/`NoContent`/`String` |
| Autenticação | um middleware (ou o handler) que lê `Authorization` ou um cookie e pode responder 401. O esquema (`bearer`, `basic`) vem do prefixo comparado no código |
| Servidor | `ListenAndServe`, `http.Server{Addr: …}`, `(*gin.Engine).Run`, echo `Start` |

Como os valores são tratados:

- **Schemas:** seguem as regras do `encoding/json` (tags, `-`, `omitempty`, `,string`, campos promovidos de structs embutidas, `MarshalJSON`/`MarshalText`, `time.Time`).
- **Obrigatoriedade:** um campo só é obrigatório quando há uma regra `required` que a decodificação aplica. Tags `binding` valem para os binds do gin; tags `validate` valem só quando a cadeia chama o validator. Ponteiros não definem obrigatoriedade.
- **Respostas:** cada uma traz status, content type, schema, origem (handler ou middleware), a cadeia de helpers até a escrita e a evidência. A base é `static` (status e corpo identificados), `partial` (um deles não) ou `unknown` (nenhum). O 200 implícito do `net/http` aparece como `possible`. Um status calculado em tempo de execução fica indeterminado, nunca é adivinhado.
- **Mapas literais:** em `gin.H{...}` e `body["code"] = …`, as chaves constantes viram campos, marcando as que só existem em alguns caminhos e os mapas que podem ganhar chaves (`maps.Copy`).
- **Tipos de parâmetro:** vêm de conversões diretas (`strconv.Atoi(c.Param("id"))` indica inteiro).

### Exemplos, cURL e exportações

- **Exemplos sintéticos:** os valores são determinísticos, escolhidos pelo tipo e pelas regras reconhecidas (`email` → `user@example.com`, `min=2`, `oneof=admin user` → `admin`, `uuid`). Quando há uma regra que o gerador não interpreta, o exemplo é marcado `unverified`. Nenhum dado vem da aplicação ou do banco.
- **cURL:** usa a base URL do endereço de escuta encontrado no código, ou um placeholder identificado como tal. Credenciais são sempre placeholders (`Bearer <token>`), o escaping é POSIX e o corpo vai em `--data-raw`.
- **OpenAPI 3.1:** `required` só para regras aplicadas, e `security` só quando o esquema é conhecido. O que não foi identificado fica em extensões `x-gitview-*`, como rotas de caminho dinâmico (`x-gitview-unresolved-routes`), status indeterminados (`default`), autenticação de nome dinâmico e evidências. Os documentos gerados para a fixture de testes e para um projeto real (134 rotas) foram validados contra o schema oficial 3.1.
- **Postman Collection v2.1:** uma pasta por grupo. Variáveis de caminho, query e headers lidos pelo código, com os opcionais desativados, e o corpo sintético. A base URL fica em `{{baseUrl}}` e o token em `{{token}}`, que começa vazio.

### No HTML

O botão **HTTP** do Repository Graph abre o explorador:

- **Catálogo (esquerda):** busca, filtro por método, agrupamento por prefixo, pacote do handler ou arquivo, e filtros de autenticação e contrato parcial. O painel pode ser recolhido.
- **Grafo de execução (centro):** a rota, a cadeia de middlewares na ordem de execução e o handler. A partir deles, os services, repositories e o SQL que podem ser chamados, coloridos pela camada do Study Mode (STATIC ou HEURISTIC). A confiança aparece em cada aresta.
  - Duplo clique expande ou recolhe uma função; a profundidade é ajustável.
  - Chamadas externas ficam ocultas por padrão.
  - Recursão aparece no Code Flow (↻).
- **Detalhes (direita), em abas:**
  - **Overview:** handler, cadeia, autenticação, camadas e SQL.
  - **Request:** construtor no estilo Postman (base URL, parâmetros, headers e corpo editáveis, *Example | Schema*), com cURL atualizado ao vivo e os botões *Copy JSON*, *Copy cURL* e *Export*. Não há botão *Send*: a página não envia requisições.
  - **Response:** cada status com schema, exemplo sintético, origem e base.
  - **Code Flow:** a árvore do grafo desenhado.
  - **Evidence:** cada relação com arquivo:linha e confiança.
- **Navegação:**
  - Qualquer função abre o Symbol Explorer, com o breadcrumb *Repository › HTTP › rota › função* para voltar.
  - *◀ Back* (Alt+←) percorre o histórico.
  - Os painéis de pacote listam as rotas do pacote.
  - `#route=…&tab=…` funciona como deep link.

A página continua um arquivo único e offline, com a mesma CSP. No HTML, cada item embute até 3 evidências e o total; o JSON (`--format json`) traz todas.

### Limites

- **Análise estática:** um caminho no grafo de execução é um caminho possível, não a execução de uma requisição.
- **Status:** um status guardado em estruturas indiretas (o campo de uma struct recebida como parâmetro, `errors.As`) fica indeterminado.
- **Profundidade:** helpers com mais de 6 chamadas de profundidade não são seguidos, e a rota é marcada como parcial.
- **Fora do alcance:**
  - frameworks além de gin, chi, echo e `net/http` (a análise é por adaptadores e pode ganhar outros, como o Fiber);
  - rotas `Static` e `NoRoute` do gin;
  - binds que dependem de um `Content-Type` decidido em tempo de execução, que aparecem com todos os decodificadores possíveis.

## Configuração (`.gitview.json` na raiz do repositório)

```json
{
  "exportDir": "reports",
  "tests": {
    "timeout": "5m",
    "projects": [
      {"name": "api", "kind": "go", "dir": "backend"},
      {"name": "e2e", "kind": "command", "dir": "web", "command": ["npm", "run", "test:e2e"]}
    ]
  },
  "runtime": {"maxCaptures": 20, "maxStorageMB": 200, "maxDuration": "5m", "interval": "500ms"}
}
```

Quando `tests.projects` está definido, ele substitui a descoberta automática. Os diretórios precisam estar dentro do repositório. A seção `runtime` só reduz os limites do Runtime Performance Inspector (o repositório não é confiável para ampliá-los); `"disabled": true` o desliga para o repositório.

## Arquitetura

| Pacote | Responsabilidade |
|---|---|
| `internal/gitdiff` | Representação estruturada dos diffs (arquivos, hunks, linhas) |
| `internal/highlight` | Tokenização Chroma compartilhada pela TUI e pelo HTML |
| `internal/search` | Busca com índice incremental sobre documentos em memória |
| `internal/export` | Markdown, HTML e JSON, detecção de dados sensíveis, gravação segura |
| `internal/testrunner` | Descoberta, adaptadores Go/Node e executor cancelável |
| `internal/config` | Leitura de `.gitview.json` |
| `internal/flow` | Índice de símbolos, call graph (SSA), rotas HTTP, operações de banco, consultas callers/callees, identidade de símbolos compartilhada |
| `internal/evidence` | Modelo de evidências (estático, observado, estimado), status de completude, identidade de símbolo, artefatos |
| `internal/inspect` | Go Deep Inspector: sessão, cache por versão do código, visões por símbolo |
| `internal/inspect/dataflow` | Data Flow: def-use do SSA, entre funções, fontes, destinos, validações |
| `internal/inspect/branch` | Branch Explorer: CFG (`go/cfg`), caminhos, métricas, sobreposição de cobertura |
| `internal/inspect/sqlops` | SQL Explorer estático: operações, SQL, parâmetros, Scan, transações, formatação, classificação, estrutura da instrução e índices declarados em `.sql` |
| `internal/inspect/pgexplain` | EXPLAIN no PostgreSQL, com conexão explícita e regras de segurança |
| `internal/inspect/perf` | Perfis pprof (decodificador próprio), benchmarks, comparação, diagnósticos do compilador |
| `internal/inspect/xtrace` | Traces de execução (`golang.org/x/exp/trace`), captura e instrumentação temporária via `-overlay` |
| `internal/inspect/run` | Execução controlada: vetores de argumentos, timeout, cancelamento, grupo de processos |
| `internal/live/proc` | Process Collector: descoberta de processos Go, métricas de `/proc`, permissões, saída e reinício |
| `internal/live/httpcol` | HTTP Collector: proxy local, cliente do collector, receptor OTLP, junção por `X-GitView-Trace-ID` |
| `internal/live/goprof` | Go Profiler: descoberta de `net/http/pprof`/collector, janelas de CPU, deltas de heap/mutex/bloqueio, conflitos |
| `internal/live/tracean` | Trace Analyzer: requisições no execution trace (tasks do collector) |
| `internal/live/srcmap` | Source Mapper: quadros, rotas e chamadas SQL → símbolos do Code Flow e operações do SQL Explorer |
| `internal/live/capture` | Sessões de captura (fases, pausa, limites) e histórico em disco |
| `internal/live/report` | Report Engine: análise por fase e por rota, base de cada número, comparação |
| `internal/graph` | Repository Graph: `model` (schema v1, catálogo HTTP), `analyzer` (Code Flow + go/types → nós e arestas), `httpapi` (contratos HTTP, exemplos, cURL, grafo de execução de rotas), `builder` (merge, graus, Louvain, camadas), `layout`, `export` (JSON, HTML com Sigma.js, OpenAPI 3.1, Postman v2.1) |
| `internal/study` | Study Mode: modelo determinístico (arquitetura, ordem de leitura, jornadas, decisões, testes, SQL, desempenho, exercícios) a partir das análises existentes |
| `internal/pbwire` | Decodificador do formato protobuf (pprof, OTLP) |
| `collector/`, `collector/pgxtracer` | Módulos opcionais (só biblioteca padrão / pgx) para instrumentar a aplicação |
| raiz (`package main`) | Componentes Bubble Tea: diff, busca, exportação, testes, Code Flow, Go Deep Inspector e Runtime Performance e Repository Graph; CLI `flow`, `inspect`, `runtime` e `graph` |
