package indexer

// EVM event signatures for Uniswap V2/V3 DEX indexing.
// These are keccak256 hashes of the canonical event signatures.

// V2 events
const (
	// PairCreated(address indexed token0, address indexed token1, address pair, uint256)
	SigPairCreated = "0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9"

	// Swap(address indexed sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, address indexed to)
	SigSwapV2 = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"

	// Mint(address indexed sender, uint256 amount0, uint256 amount1)
	SigMintV2 = "0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f"

	// Burn(address indexed sender, uint256 amount0, uint256 amount1, address indexed to)
	SigBurnV2 = "0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496"

	// Sync(uint112 reserve0, uint112 reserve1)
	SigSync = "0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1"

	// Transfer(address indexed from, address indexed to, uint256 value)
	SigTransfer = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
)

// V3 events
const (
	// PoolCreated(address indexed token0, address indexed token1, uint24 indexed fee, int24 tickSpacing, address pool)
	SigPoolCreated = "0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118"

	// Initialize(uint160 sqrtPriceX96, int24 tick)
	SigInitialize = "0x98636036cb66a9c19a37435efc1e90142190214e8abeb821bdba3f2990dd4c95"

	// Swap(address indexed sender, address indexed recipient, int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
	SigSwapV3 = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"

	// Mint(address sender, address indexed owner, int24 indexed tickLower, int24 indexed tickUpper, uint128 amount, uint256 amount0, uint256 amount1)
	SigMintV3 = "0x7a53080ba414158be7ec69b987b5fb7d07dee101fe85488f0853ae16239d0bde"

	// Burn(address indexed owner, int24 indexed tickLower, int24 indexed tickUpper, uint128 amount, uint256 amount0, uint256 amount1)
	SigBurnV3 = "0x0c396cd989a39f4459b5fa1aed6a9a8dcdbc45908acfd67e028cd568da98982c"

	// Collect(address indexed owner, address recipient, int24 indexed tickLower, int24 indexed tickUpper, uint128 amount0, uint128 amount1)
	SigCollect = "0x70935338e69775456b0c0043e55b3188a159c4f5e6de2a46547cb5286cd0a097"

	// Flash(address indexed sender, address indexed recipient, uint256 amount0, uint256 amount1, uint256 paid0, uint256 paid1)
	SigFlash = "0xbdbdb71d7860376ba52b25a5028beea23581364a40522f6bcfb86bb1f2dca633"
)

// V4 events (PoolManager singleton pattern)
const (
	// Initialize(bytes32 indexed id, address indexed currency0, address indexed currency1, uint24 fee, int24 tickSpacing, address hooks, uint160 sqrtPriceX96, int24 tick)
	SigInitializeV4 = "0x344560c924012b32cbed54ad0e83e24c2cf3e723de46e06c72e6ab3463a7a8c0"

	// ModifyLiquidity(bytes32 indexed id, address indexed sender, int24 tickLower, int24 tickUpper, int256 liquidityDelta, bytes32 salt)
	SigModifyLiquidity = "0xf208f4912782fd87d4a358e8291c46e9fefb649e838a67dcaa5db23503a4f53b"

	// Swap(bytes32 indexed id, address indexed sender, int128 amount0, int128 amount1, uint160 sqrtPriceX96After, uint128 liquidityAfter, int24 tickAfter)
	SigSwapV4 = "0x40e9cecb9f5f1f1c5b9c97dec2917b7ee92e57ba5563708daca94dd84ad7112f"

	// Subscription(uint256 indexed tokenId, address indexed subscriber)
	SigSubscription = "0x572f161235911da04685a68c6b07422b4ba12dab20405b8b230c2420f49f1266"

	// Unsubscription(uint256 indexed tokenId, address indexed subscriber)
	SigUnsubscription = "0x6112cc683e05f5c607f9d5f0f57a0f0be2ca872a8f94ed4b6e2d09da2f1d1882"
)

// Known factory addresses per network.
type NetworkConfig struct {
	Name           string `json:"name" yaml:"name"`
	ChainID        int64  `json:"chainId" yaml:"chain_id"`
	FactoryV2      string `json:"factoryV2" yaml:"factory_v2"`
	FactoryV3      string `json:"factoryV3" yaml:"factory_v3"`
	WETH           string `json:"weth" yaml:"weth"`             // wrapped native token
	StableTokens   []string `json:"stableTokens" yaml:"stable_tokens"`
}

// Lux network configs — same addresses as exchange subgraphs.
var LuxMainnet = NetworkConfig{
	Name:      "lux",
	ChainID:   96369,
	FactoryV2: "0xD173926A10A0C4eCd3A51B1422270b65Df0551c1",
	FactoryV3: "0x80bBc7C4C7a59C899D1B37BC14539A22D5830a84",
	WETH:      "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e", // WLUX
	StableTokens: []string{
		"0x848Cff46eb323f323b6Bbe1Df274E40793d7f2c2", // LUSD
	},
}

var ZooMainnet = NetworkConfig{
	Name:      "zoo",
	ChainID:   200200,
	FactoryV2: "0xF034942c1140125b5c278aE9cEE1B488e915B2FE",
	FactoryV3: "0x80bBc7C4C7a59C899D1B37BC14539A22D5830a84",
	WETH:      "0x5491216406daB99b7032b83765F36790E27F8A61", // WLUX
	StableTokens: []string{
		"0xb2ee1CE7b84853b83AA08702aD0aD4D79711882D", // LUSDC
	},
}
