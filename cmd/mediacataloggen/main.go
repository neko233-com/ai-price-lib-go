package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/neko233-com/ai-price-lib-go/catalog"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: mediacataloggen source.json output.gz")
	}
	f, e := os.Open(os.Args[1])
	if e != nil {
		panic(e)
	}
	defer f.Close()
	c, e := catalog.ParseLiteLLM(f, time.Now())
	if e != nil {
		panic(e)
	}
	out, e := os.Create(os.Args[2])
	if e != nil {
		panic(e)
	}
	z := gzip.NewWriter(out)
	if e = json.NewEncoder(z).Encode(c); e != nil {
		panic(e)
	}
	if e = z.Close(); e != nil {
		panic(e)
	}
	if e = out.Close(); e != nil {
		panic(e)
	}
	fmt.Printf("media_prices=%d retrieved_at=%s\n", len(c.Prices), c.RetrievedAt)
}
