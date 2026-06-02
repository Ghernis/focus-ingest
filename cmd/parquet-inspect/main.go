package main

import (
	"fmt"
	"os"

	"github.com/parquet-go/parquet-go"
)

func main() {
	path := `data/55624b94-6c2b-4865-b13b-1a44e941c5eb__part_2_0001.snappy.parquet`
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		panic(err)
	}
	pf, err := parquet.OpenFile(f, stat.Size())
	if err != nil {
		panic(err)
	}
	fmt.Println("rows:", pf.NumRows())
	for _, c := range pf.Schema().Columns() {
		fmt.Printf("%v\n", c)
	}
}
