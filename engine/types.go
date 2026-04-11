package engine

// Entity types matching Uniswap V2/V3 subgraph schemas exactly.
// Field names match the GraphQL schema so the exchange frontend
// queries work without modification.

// Factory — V3 factory or V2 UniswapFactory
type Factory struct {
	ID                  string `json:"id"`
	PoolCount           int64  `json:"poolCount"`
	PairCount           int64  `json:"pairCount"`           // v2
	TxCount             int64  `json:"txCount"`
	TotalVolumeUSD      string `json:"totalVolumeUSD"`
	TotalVolumeETH      string `json:"totalVolumeETH"`
	TotalFeesUSD        string `json:"totalFeesUSD"`
	TotalValueLockedUSD string `json:"totalValueLockedUSD"`
	TotalLiquidityUSD   string `json:"totalLiquidityUSD"`   // v2 compat
	TotalValueLockedETH string `json:"totalValueLockedETH"`
}

// Bundle — native token price in USD
type Bundle struct {
	ID          string `json:"id"`
	EthPriceUSD string `json:"ethPriceUSD"`
	EthPrice    string `json:"ethPrice"`    // v2 compat
	LuxPriceUSD string `json:"luxPriceUSD"`
}

// Token — ERC20 token
type Token struct {
	ID                  string `json:"id"`
	Symbol              string `json:"symbol"`
	Name                string `json:"name"`
	Decimals            int64  `json:"decimals"`
	TotalSupply         string `json:"totalSupply"`
	Volume              string `json:"volume"`
	VolumeUSD           string `json:"volumeUSD"`
	UntrackedVolumeUSD  string `json:"untrackedVolumeUSD"`
	FeesUSD             string `json:"feesUSD"`
	TxCount             int64  `json:"txCount"`
	PoolCount           int64  `json:"poolCount"`
	TotalValueLocked    string `json:"totalValueLocked"`
	TotalValueLockedUSD string `json:"totalValueLockedUSD"`
	TotalLiquidity      string `json:"totalLiquidity"`      // v2
	DerivedETH          string `json:"derivedETH"`
	TradeVolume         string `json:"tradeVolume"`         // v2
	TradeVolumeUSD      string `json:"tradeVolumeUSD"`      // v2
}

// Pool — V3 concentrated liquidity pool
type Pool struct {
	ID                     string `json:"id"`
	CreatedAtTimestamp     int64  `json:"createdAtTimestamp"`
	CreatedAtBlockNumber   int64  `json:"createdAtBlockNumber"`
	Token0                 *Token `json:"token0"`
	Token1                 *Token `json:"token1"`
	FeeTier                int64  `json:"feeTier"`
	Liquidity              string `json:"liquidity"`
	SqrtPrice              string `json:"sqrtPrice"`
	Token0Price            string `json:"token0Price"`
	Token1Price            string `json:"token1Price"`
	Tick                   int64  `json:"tick"`
	VolumeToken0           string `json:"volumeToken0"`
	VolumeToken1           string `json:"volumeToken1"`
	VolumeUSD              string `json:"volumeUSD"`
	FeesUSD                string `json:"feesUSD"`
	TxCount                int64  `json:"txCount"`
	TotalValueLockedToken0 string `json:"totalValueLockedToken0"`
	TotalValueLockedToken1 string `json:"totalValueLockedToken1"`
	TotalValueLockedETH    string `json:"totalValueLockedETH"`
	TotalValueLockedUSD    string `json:"totalValueLockedUSD"`
}

// Pair — V2 constant product AMM pair
type Pair struct {
	ID                   string `json:"id"`
	Token0               *Token `json:"token0"`
	Token1               *Token `json:"token1"`
	Reserve0             string `json:"reserve0"`
	Reserve1             string `json:"reserve1"`
	TotalSupply          string `json:"totalSupply"`
	ReserveETH           string `json:"reserveETH"`
	ReserveUSD           string `json:"reserveUSD"`
	TrackedReserveETH    string `json:"trackedReserveETH"`
	Token0Price          string `json:"token0Price"`
	Token1Price          string `json:"token1Price"`
	VolumeToken0         string `json:"volumeToken0"`
	VolumeToken1         string `json:"volumeToken1"`
	VolumeUSD            string `json:"volumeUSD"`
	TxCount              int64  `json:"txCount"`
	CreatedAtTimestamp   int64  `json:"createdAtTimestamp"`
	CreatedAtBlockNumber int64  `json:"createdAtBlockNumber"`
}

// Swap — trade event (V2 and V3)
type Swap struct {
	ID          string `json:"id"`
	Transaction string `json:"transaction"`
	Timestamp   int64  `json:"timestamp"`
	Pool        string `json:"pool"`
	Pair        string `json:"pair"`      // v2
	Token0      string `json:"token0"`
	Token1      string `json:"token1"`
	Sender      string `json:"sender"`
	Recipient   string `json:"recipient"`
	Origin      string `json:"origin"`
	Amount0     string `json:"amount0"`
	Amount1     string `json:"amount1"`
	AmountUSD   string `json:"amountUSD"`
	Amount0In   string `json:"amount0In"`  // v2
	Amount0Out  string `json:"amount0Out"` // v2
	Amount1In   string `json:"amount1In"`  // v2
	Amount1Out  string `json:"amount1Out"` // v2
}

// Mint — liquidity provision event
type Mint struct {
	ID          string `json:"id"`
	Transaction string `json:"transaction"`
	Timestamp   int64  `json:"timestamp"`
	Pool        string `json:"pool"`
	Pair        string `json:"pair"` // v2
	Sender      string `json:"sender"`
	Owner       string `json:"owner"`
	Amount0     string `json:"amount0"`
	Amount1     string `json:"amount1"`
	AmountUSD   string `json:"amountUSD"`
	TickLower   int64  `json:"tickLower"` // v3
	TickUpper   int64  `json:"tickUpper"` // v3
	LogIndex    int64  `json:"logIndex"`
}

// Burn — liquidity removal event
type Burn struct {
	ID          string `json:"id"`
	Transaction string `json:"transaction"`
	Timestamp   int64  `json:"timestamp"`
	Pool        string `json:"pool"`
	Pair        string `json:"pair"` // v2
	Owner       string `json:"owner"`
	Amount0     string `json:"amount0"`
	Amount1     string `json:"amount1"`
	AmountUSD   string `json:"amountUSD"`
	TickLower   int64  `json:"tickLower"` // v3
	TickUpper   int64  `json:"tickUpper"` // v3
	LogIndex    int64  `json:"logIndex"`
}

// Tick — V3 price tick
type Tick struct {
	ID             string `json:"id"`
	PoolAddress    string `json:"poolAddress"`
	TickIdx        int64  `json:"tickIdx"`
	LiquidityGross string `json:"liquidityGross"`
	LiquidityNet   string `json:"liquidityNet"`
	Price0         string `json:"price0"`
	Price1         string `json:"price1"`
}

// Time-series entities

type TokenDayData struct {
	ID                  string `json:"id"`
	Date                int64  `json:"date"`
	Token               *Token `json:"token"`
	Volume              string `json:"volume"`
	VolumeUSD           string `json:"volumeUSD"`
	TotalValueLocked    string `json:"totalValueLocked"`
	TotalValueLockedUSD string `json:"totalValueLockedUSD"`
	PriceUSD            string `json:"priceUSD"`
	Open                string `json:"open"`
	High                string `json:"high"`
	Low                 string `json:"low"`
	Close               string `json:"close"`
}

type PoolDayData struct {
	ID                  string `json:"id"`
	Date                int64  `json:"date"`
	Pool                *Pool  `json:"pool"`
	Liquidity           string `json:"liquidity"`
	VolumeUSD           string `json:"volumeUSD"`
	FeesUSD             string `json:"feesUSD"`
	TotalValueLockedUSD string `json:"tvlUSD"`
	TxCount             int64  `json:"txCount"`
	Open                string `json:"open"`
	High                string `json:"high"`
	Low                 string `json:"low"`
	Close               string `json:"close"`
}

type PairDayData struct {
	ID        string `json:"id"`
	Date      int64  `json:"date"`
	Pair      *Pair  `json:"pair"`
	VolumeUSD string `json:"dailyVolumeUSD"`
	ReserveUSD string `json:"reserveUSD"`
	TxCount   int64  `json:"dailyTxns"`
}

type FactoryDayData struct {
	ID                  string `json:"id"`
	Date                int64  `json:"date"`
	VolumeUSD           string `json:"dailyVolumeUSD"`
	TotalValueLockedUSD string `json:"totalLiquidityUSD"`
	TxCount             int64  `json:"dailyTxns"`
}

// Transaction — on-chain transaction
type Transaction struct {
	ID          string `json:"id"`
	BlockNumber int64  `json:"blockNumber"`
	Timestamp   int64  `json:"timestamp"`
	GasUsed     string `json:"gasUsed"`
	GasPrice    string `json:"gasPrice"`
}

// Collect — V3 fee collection event
type Collect struct {
	ID          string `json:"id"`
	Transaction string `json:"transaction"`
	Timestamp   int64  `json:"timestamp"`
	Pool        string `json:"pool"`
	Owner       string `json:"owner"`
	Amount0     string `json:"amount0"`
	Amount1     string `json:"amount1"`
	TickLower   int64  `json:"tickLower"`
	TickUpper   int64  `json:"tickUpper"`
}

// Flash — V3 flash loan event
type Flash struct {
	ID          string `json:"id"`
	Transaction string `json:"transaction"`
	Timestamp   int64  `json:"timestamp"`
	Pool        string `json:"pool"`
	Sender      string `json:"sender"`
	Recipient   string `json:"recipient"`
	Amount0     string `json:"amount0"`
	Amount1     string `json:"amount1"`
	Paid0       string `json:"paid0"`
	Paid1       string `json:"paid1"`
}

// PairHourData — hourly V2 pair snapshot
type PairHourData struct {
	ID              string `json:"id"`
	PeriodStartUnix int64  `json:"periodStartUnix"`
	Pair            *Pair  `json:"pair"`
	Reserve0        string `json:"reserve0"`
	Reserve1        string `json:"reserve1"`
	ReserveUSD      string `json:"reserveUSD"`
	VolumeUSD       string `json:"volumeUSD"`
	TxCount         int64  `json:"txCount"`
}
