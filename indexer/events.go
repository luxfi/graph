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

// ─────────────────────────────────────────────────────────────────────────────
// ERC-3643 (T-REX) `IToken` events
// ─────────────────────────────────────────────────────────────────────────────
const (
	// AddressFrozen(address indexed user, bool indexed isFrozen, address indexed owner)
	SigAddressFrozen = "0x7fa523c84ab8d7fc5b72f08b9e46dbbf10c39e119a075b3e317002d14bc9f436"

	// TokensFrozen(address indexed user, uint256 amount)
	SigTokensFrozen = "0xa065e63c631c86f1b9f66a4a2f63f2093bf1c2168d23290259dbd969e0222a45"

	// TokensUnfrozen(address indexed user, uint256 amount)
	SigTokensUnfrozen = "0x9bed35cb62ad0dba04f9d5bfee4b5bc91443e77da8a65c4c84834c51bb08b0d6"

	// Paused(address user) — T-REX token pause
	SigSecurityPaused = "0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258"

	// Unpaused(address user)
	SigSecurityUnpaused = "0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa"

	// RecoverySuccess(address indexed lostWallet, address indexed newWallet, address indexed investorOnchainID)
	SigRecoverySuccess = "0xf0c9129a94f30f1caaceb63e44b9811d0a3edf1d6c23757f346093af5553fed0"

	// UpdatedTokenInformation(string indexed name, string indexed symbol, uint8 decimals, string version, address indexed onchainID)
	SigUpdatedTokenInformation = "0x6a1105ac8148a3c319adbc369f9072573e8a11d3a3d195e067e7c40767ec54d1"

	// IdentityRegistryAdded(address indexed identityRegistry)
	SigIdentityRegistryAdded = "0xd2be862d755bca7e0d39772b2cab3a5578da9c285f69199f4c063c2294a7f36c"

	// ComplianceAdded(address indexed compliance)
	SigComplianceAdded = "0x7f3a888862559648ec01d97deb7b5012bff86dc91e654a1de397170db40e35b6"
)

// ─────────────────────────────────────────────────────────────────────────────
// ERC-3643 IdentityRegistry / IdentityRegistryStorage
// ─────────────────────────────────────────────────────────────────────────────
const (
	SigIdentityRegistered = "0x6ae73635c50d24a45af6fbd5e016ac4bed179addbc8bf24e04ff0fcc6d33af19"
	SigIdentityRemoved    = "0x59d6590e225b81befe259af056324092801080acbb7feab310eb34678871f327"
	SigIdentityUpdated    = "0xe98082932c8056a0f514da9104e4a66bc2cbaef102ad59d90c4b24220ebf6010"
	SigCountryUpdated     = "0x04ed3b726495c2dca1ff1215d9ca54e1a4030abb5e82b0f6ce55702416cee853"
	SigIdentityStored     = "0x0030dea7e9c9afaa2e3c9810f2fc9b5181f1bad74ca5a8db85f746a33585e747"
)

// ─────────────────────────────────────────────────────────────────────────────
// ONCHAINID  — ERC-734 (key mgmt) + ERC-735 (claims)
// ─────────────────────────────────────────────────────────────────────────────
const (
	// ClaimAdded/Removed/Changed(bytes32 indexed claimId, uint256 indexed topic, uint256 scheme, address indexed issuer, bytes signature, bytes data, string uri)
	SigClaimAdded   = "0x46149b18aa084502c3f12bc75e19eda8bda8d102b82cce8474677a6d0d5f43c5"
	SigClaimRemoved = "0x3cf57863a89432c61c4a27073c6ee39e8a764bff5a05aebfbcdcdc80b2e6130a"
	SigClaimChanged = "0x3bab293fc00db832d7619a9299914251b8747c036867ec056cbd506f60135b13"

	// KeyAdded/Removed(bytes32 indexed key, uint256 indexed purpose, uint256 indexed keyType)
	SigKeyAdded   = "0x480000bb1edad8ca1470381cc334b1917fbd51c6531f3a623ea8e0ec7e38a6e9"
	SigKeyRemoved = "0x585a4aef50f8267a92b32412b331b20f7f8b96f2245b253b9cc50dcc621d3397"

	// Approved(uint256 indexed executionId, bool approved)
	SigOnchainIdApproved = "0xb3932da477fe5d6c8ff2eafef050c0f3a1af18fc07121001482600f36f3715d8"

	// Executed(uint256 indexed executionId, address indexed to, uint256 indexed value, bytes data)
	SigOnchainIdExecuted = "0x1f920dbda597d7bf95035464170fa58d0a4b57f13a1c315ace6793b9f63688b8"
)

// ─────────────────────────────────────────────────────────────────────────────
// ERC-3643 TrustedIssuersRegistry / ClaimTopicsRegistry
// ─────────────────────────────────────────────────────────────────────────────
const (
	SigTrustedIssuerAdded   = "0xfedc33fd34859594822c0ff6f3f4f9fc279cc6d5cae53068f706a088e4500872"
	SigTrustedIssuerRemoved = "0x2214ded40113cc3fb63fc206cafee88270b0a903dac7245d54efdde30ebb0321"
	SigClaimTopicsUpdated   = "0xec753cfc52044f61676f18a11e500093a9f2b1cd5e4942bc476f2b0438159bcf"
	SigClaimTopicAdded      = "0x01c928b7f7ade2949e92366aa9454dbef3a416b731cf6ec786ba9595bbd814d6"
	SigClaimTopicRemoved    = "0x0b1381093c776453c1bbe54fd68be1b235c65db61d099cb50d194b2991e0eec5"
)

// ─────────────────────────────────────────────────────────────────────────────
// ERC-3643 ModularCompliance + IModule
// ─────────────────────────────────────────────────────────────────────────────
const (
	SigModuleAdded       = "0xead6a006345da1073a106d5f32372d2d2204f46cb0b4bca8f5ebafcbbed12b8a"
	SigModuleRemoved     = "0x0a1ee69f55c33d8467c69ca59ce2007a737a88603d75392972520bf67cb513b8"
	SigTokenBound        = "0x2de35142b19ed5a07796cf30791959c592018f70b1d2d7c460eef8ffe713692b"
	SigTokenUnbound      = "0x28a4ca7134a3b3f9aff286e79ad3daadb4a06d1b43d037a3a98bdc074edd9b7a"
	SigModuleInteraction = "0x20d79de70adcc6e9353d8a9a5646b46dc352710d0a310b1ad1f67faeca7ef891"
)

// SecuritiesTopics returns every topic0 indexed by the securities resolvers.
// Use this when constructing eth_getLogs filters or extending knownTopics().
func SecuritiesTopics() []string {
	return []string{
		// IToken
		SigAddressFrozen, SigTokensFrozen, SigTokensUnfrozen,
		SigSecurityPaused, SigSecurityUnpaused, SigRecoverySuccess,
		SigUpdatedTokenInformation, SigIdentityRegistryAdded, SigComplianceAdded,
		// IdentityRegistry / Storage
		SigIdentityRegistered, SigIdentityRemoved, SigIdentityUpdated,
		SigCountryUpdated, SigIdentityStored,
		// ONCHAINID
		SigClaimAdded, SigClaimRemoved, SigClaimChanged,
		SigKeyAdded, SigKeyRemoved,
		SigOnchainIdApproved, SigOnchainIdExecuted,
		// Trusted issuers / topics registries
		SigTrustedIssuerAdded, SigTrustedIssuerRemoved, SigClaimTopicsUpdated,
		SigClaimTopicAdded, SigClaimTopicRemoved,
		// ModularCompliance + IModule
		SigModuleAdded, SigModuleRemoved, SigTokenBound, SigTokenUnbound, SigModuleInteraction,
	}
}

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
