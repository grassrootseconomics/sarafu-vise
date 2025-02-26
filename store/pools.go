package store

import (
	"fmt"
	"strings"

	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

// PoolsMetadata helps organize data fields
type PoolsMetadata struct {
	PoolNames             string
	PoolSymbols           string
	PoolContractAdrresses string
}

// ProcessPools converts pools into formatted strings
func ProcessPools(pools []dataserviceapi.PoolDetails) PoolsMetadata {
	var data PoolsMetadata
	var poolNames, poolSymbols, poolContractAdrresses []string

	for i, p := range pools {
		poolNames = append(poolNames, fmt.Sprintf("%d:%s", i+1, p.PoolName))
		poolSymbols = append(poolSymbols, fmt.Sprintf("%d:%s", i+1,  p.PoolSymbol))
		poolContractAdrresses = append(poolContractAdrresses, fmt.Sprintf("%d:%s", i+1,  p.PoolContractAdrress))
	}

	data.PoolNames = strings.Join(poolNames, "\n")
	data.PoolSymbols = strings.Join(poolSymbols, "\n")
	data.PoolContractAdrresses = strings.Join(poolContractAdrresses, "\n")

	return data
}
