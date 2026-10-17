# GitView collector

Instrumentação opcional para servidores HTTP em Go, usada pelo **Runtime Performance Inspector** do GitView (`gitview runtime`, tecla `m`). Sem ela, o GitView já mede o processo pelo PID e as requisições pelo proxy local; com ela, ganha:

- o padrão da rota (`GET /clients/{id}`) e o tempo dentro do handler;
- cada instrução SQL com a linha de código que a executou (relacionada ao SQL Explorer);
- o tempo de CPU de cada requisição (amostras do perfil de CPU com um label da requisição);
- uma task do `runtime/trace` por requisição, que separa o execution trace por requisição;
- leituras de `runtime/metrics` (GC, heap, goroutines, latência do scheduler) e perfis sem `net/http/pprof`.

Só usa a biblioteca padrão. Fica inerte até o GitView armá-lo para uma captura: parado, custa uma leitura atômica por requisição.

## Instalação

```go
import "github.com/athos/gitview/collector"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /clients/{id}", getClient)

	c := collector.Default()
	if _, err := c.Start(""); err != nil { // endpoints do GitView em 127.0.0.1, porta livre
		log.Print("gitview collector: ", err)
	}
	defer c.Close()

	http.ListenAndServe(":8080", c.Middleware(mux))
}
```

Enquanto o módulo não é publicado, use um `replace` para a cópia local do GitView:

```
require github.com/athos/gitview/collector v0.0.0
replace github.com/athos/gitview/collector => /caminho/do/gitview/collector
```

### Rotas de outros roteadores

O padrão da rota do `http.ServeMux` é lido automaticamente quando o middleware envolve o mux. Para outros roteadores, informe-o depois do roteamento. Com chi:

```go
r.Use(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req)
		collector.SetRoute(req.Context(), chi.RouteContext(req.Context()).RoutePattern())
	})
})
```

Sem o padrão, o GitView agrupa as requisições pelo caminho, trocando números e UUIDs por marcadores.

### SQL

Com pgx v5, use o módulo `collector/pgxtracer`:

```go
cfg, _ := pgxpool.ParseConfig(dsn)
cfg.ConnConfig.Tracer = pgxtracer.New()
```

Com outra biblioteca, marque a instrução no ponto em que ela é executada:

```go
span := collector.SQL(ctx, query)
rows, err := db.QueryContext(ctx, query, args...)
span.End(err)
```

A linha registrada é a primeira fora do collector, do `database/sql` e dos drivers comuns. Só o texto da instrução é registrado, sem literais (trocados por `?`) e sem comentários; os argumentos nunca são lidos.

## O que nunca é registrado

Cabeçalhos (cookies, `Authorization`), query strings, corpos de requisição e de resposta, argumentos e literais de SQL e mensagens de erro (só o tipo do erro). Do caminho, só ficam segmentos que são palavras (`clients`, `v2`); os demais viram `{n}`, `{uuid}`, `{hex}` ou `{value}`. Cada requisição recebe um ID próprio: um `X-GitView-Trace-ID` repetido é substituído e um `traceparent` é guardado como trace, não como ID da requisição.

## Segurança

- `Start` só escuta em endereços de loopback. Os endpoints (`/debug/gitview/…`) recusam clientes que não sejam loopback, requisições com `Forwarded`/`X-Forwarded-For`/`X-Real-IP` e qualquer requisição sem o token.
- O token é aleatório por processo e fica em `$XDG_RUNTIME_DIR/gitview/collectors/<pid>.json` (ou no diretório de cache do usuário, se `XDG_RUNTIME_DIR` não for um diretório privado do usuário), criado com exclusividade e permissão `0600` num diretório `0700` do próprio usuário; só o mesmo usuário o lê. O arquivo é removido em `Close`.
- `Handler()` pode ser montado no mux da aplicação, mas prefira `Start`: num mux público, um proxy reverso na mesma máquina faria as requisições chegarem como loopback (por isso as com cabeçalhos de proxy são recusadas).
- `net/http/pprof` não é importado: os perfis são servidos pelo próprio collector, atrás do token, e nada é registrado no `http.DefaultServeMux`.
- O GitView arma o collector com prazo (no máximo 1 hora) e recebe uma concessão: outra captura não consegue armá-lo, desarmá-lo nem mudar taxas enquanto ela vale; uma captura abandonada se desarma sozinha. Taxas de perfil de mutex e bloqueio só mudam durante uma captura armada; quando ela termina, a de mutex volta ao valor anterior e a de bloqueio volta ao valor definido pela aplicação com `collector.Default().SetBlockProfileRate` (o runtime não informa a taxa anterior; sem essa chamada, volta a 0).
- `GITVIEW_COLLECTOR=off` desativa tudo: o middleware devolve o handler original e nenhum endpoint é servido.

## Custo enquanto armado

Por requisição: duas leituras de relógio, um identificador aleatório, o cabeçalho `X-GitView-Trace-ID` na resposta, um label de pprof, uma task de `runtime/trace` (barata sem trace ativo) e uma leitura de `runtime/metrics`. O registro fica num buffer circular de 4096 requisições; o GitView o lê a cada 500 ms e avisa se algo se perdeu.
