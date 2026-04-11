package engine

// V4 entity types — Uniswap V4 (PoolManager singleton, hooks, ModifyLiquidity).
// Extends the V3 types. Pool/Token/Bundle/Tick are shared across V2/V3/V4.

// PoolManager — V4 singleton (replaces per-factory pattern)
type PoolManager struct {
	ID                             string `json:"id"`
	PoolCount                      int64  `json:"poolCount"`
	TxCount                        int64  `json:"txCount"`
	TotalVolumeUSD                 string `json:"totalVolumeUSD"`
	TotalVolumeETH                 string `json:"totalVolumeETH"`
	TotalFeesUSD                   string `json:"totalFeesUSD"`
	TotalFeesETH                   string `json:"totalFeesETH"`
	UntrackedVolumeUSD             string `json:"untrackedVolumeUSD"`
	TotalValueLockedUSD            string `json:"totalValueLockedUSD"`
	TotalValueLockedETH            string `json:"totalValueLockedETH"`
	TotalValueLockedUSDUntracked   string `json:"totalValueLockedUSDUntracked"`
	TotalValueLockedETHUntracked   string `json:"totalValueLockedETHUntracked"`
	Owner                          string `json:"owner"`
}

// ModifyLiquidity — V4 unified liquidity event (replaces separate Mint/Burn)
type ModifyLiquidity struct {
	ID          string `json:"id"`
	Transaction string `json:"transaction"`
	Timestamp   int64  `json:"timestamp"`
	Pool        string `json:"pool"`
	Token0      string `json:"token0"`
	Token1      string `json:"token1"`
	Sender      string `json:"sender"`
	Origin      string `json:"origin"`
	Amount      string `json:"amount"`      // liquidityDelta (positive=mint, negative=burn)
	Amount0     string `json:"amount0"`
	Amount1     string `json:"amount1"`
	AmountUSD   string `json:"amountUSD"`
	TickLower   int64  `json:"tickLower"`
	TickUpper   int64  `json:"tickUpper"`
	LogIndex    int64  `json:"logIndex"`
}

// Position — V4 NFT position (ERC721 tokenId)
type Position struct {
	ID                 string `json:"id"`
	TokenID            string `json:"tokenId"`
	Owner              string `json:"owner"`
	Origin             string `json:"origin"`
	CreatedAtTimestamp int64  `json:"createdAtTimestamp"`
}

// Subscribe — V4 hook subscription event
type Subscribe struct {
	ID        string `json:"id"`
	TokenID   string `json:"tokenId"`
	Address   string `json:"address"`
	Timestamp int64  `json:"timestamp"`
	Origin    string `json:"origin"`
	LogIndex  int64  `json:"logIndex"`
}

// Unsubscribe — V4 hook unsubscription event
type Unsubscribe struct {
	ID        string `json:"id"`
	TokenID   string `json:"tokenId"`
	Address   string `json:"address"`
	Timestamp int64  `json:"timestamp"`
	Origin    string `json:"origin"`
	LogIndex  int64  `json:"logIndex"`
}

// PoolV4 extends Pool with V4-specific fields
type PoolV4 struct {
	Pool
	TickSpacing          int64  `json:"tickSpacing"`
	Hooks                string `json:"hooks"`
	IsExternalLiquidity  bool   `json:"isExternalLiquidity"`
	CollectedFeesToken0  string `json:"collectedFeesToken0"`
	CollectedFeesToken1  string `json:"collectedFeesToken1"`
	CollectedFeesUSD     string `json:"collectedFeesUSD"`
	LiquidityProviderCount int64 `json:"liquidityProviderCount"`
}

// UniswapDayData — V4 daily aggregate (same name as V3 for compat)
type UniswapDayData struct {
	ID          string `json:"id"`
	Date        int64  `json:"date"`
	VolumeETH   string `json:"volumeETH"`
	VolumeUSD   string `json:"volumeUSD"`
	FeesUSD     string `json:"feesUSD"`
	TxCount     int64  `json:"txCount"`
	TvlUSD      string `json:"tvlUSD"`
}

// PoolHourData — hourly pool snapshot
type PoolHourData struct {
	ID              string `json:"id"`
	PeriodStartUnix int64  `json:"periodStartUnix"`
	Pool            *Pool  `json:"pool"`
	Liquidity       string `json:"liquidity"`
	VolumeUSD       string `json:"volumeUSD"`
	FeesUSD         string `json:"feesUSD"`
	TvlUSD          string `json:"tvlUSD"`
	TxCount         int64  `json:"txCount"`
	Open            string `json:"open"`
	High            string `json:"high"`
	Low             string `json:"low"`
	Close           string `json:"close"`
}

// TokenHourData — hourly token snapshot
type TokenHourData struct {
	ID                  string `json:"id"`
	PeriodStartUnix     int64  `json:"periodStartUnix"`
	Token               *Token `json:"token"`
	Volume              string `json:"volume"`
	VolumeUSD           string `json:"volumeUSD"`
	TotalValueLocked    string `json:"totalValueLocked"`
	TotalValueLockedUSD string `json:"totalValueLockedUSD"`
	PriceUSD            string `json:"priceUSD"`
	FeesUSD             string `json:"feesUSD"`
	Open                string `json:"open"`
	High                string `json:"high"`
	Low                 string `json:"low"`
	Close               string `json:"close"`
}

// Hook entities

type EulerSwapHook struct {
	ID           string `json:"id"`
	Hook         string `json:"hook"`
	EulerAccount string `json:"eulerAccount"`
	Asset0       string `json:"asset0"`
	Asset1       string `json:"asset1"`
}

type ArrakisHook struct {
	ID                   string `json:"id"`
	Module               string `json:"module"`
	Salt                 string `json:"salt"`
	CreatedAtTimestamp   int64  `json:"createdAtTimestamp"`
	CreatedAtBlockNumber int64  `json:"createdAtBlockNumber"`
}
