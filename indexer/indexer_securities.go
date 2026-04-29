package indexer

// ERC-3643 + ONCHAINID event handlers.
//
// Each handler decodes the relevant log topics/data and writes a structured
// entity through the storage layer. Resolvers in resolvers/securities/ read
// these entities back.
//
// Entity types written here (read by resolvers/securities/securities.go):
//   - SecurityTransfer            (already handled by handleTransfer for IToken Transfer)
//   - TransferAgentAction         (AddressFrozen / TokensFrozen / TokensUnfrozen / RecoverySuccess / Paused / Unpaused)
//   - FrozenAccount               (current AddressFrozen state per (token, user))
//   - FrozenTokens                (current per-(token, user) partial-freeze amount)
//   - IdentityRegistryAction      (IdentityRegistered/Removed/Updated/Stored, CountryUpdated)
//   - OnchainIdClaim              (ClaimAdded/Removed/Changed)
//   - OnchainIdKey                (KeyAdded/Removed)
//   - OnchainIdApproval           (Approved/Executed)
//   - TrustedIssuerAction         (TrustedIssuerAdded/Removed, ClaimTopicsUpdated)
//   - ClaimTopicAction            (ClaimTopicAdded/Removed)
//   - ComplianceAction            (ModuleAdded/Removed, TokenBound/Unbound, ModuleInteraction)

import (
	"fmt"
	"math/big"
	"strings"
)

// topicBool decodes a bool indexed topic (`bytes32(0)` = false, anything else = true).
func topicBool(topic string) bool {
	t := strings.TrimPrefix(topic, "0x")
	for i := 0; i < len(t); i++ {
		if t[i] != '0' {
			return true
		}
	}
	return false
}

// decodeUint256Topic decodes a uint256 from an indexed topic (32-byte word).
func decodeUint256Topic(topic string) *big.Int {
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(topic, "0x"), 16)
	return n
}

// decodeAddress reads a 32-byte word at wordIndex and returns the low 20 bytes as a 0x-prefixed address.
func decodeAddress(data string, wordIndex int) string {
	d := strings.TrimPrefix(data, "0x")
	start := wordIndex * 64
	if start+64 > len(d) {
		return "0x0000000000000000000000000000000000000000"
	}
	return "0x" + d[start+24:start+64]
}

// id makes a stable per-log entity id: <token>:<txHash>:<logIdx>.
func entityID(addr, txHash, logIdx string) string {
	return fmt.Sprintf("%s:%s:%s", addr, txHash, logIdx)
}

// ── ERC-3643 IToken ──────────────────────────────────────────────────────────

// AddressFrozen(address indexed user, bool indexed isFrozen, address indexed owner)
func (idx *Indexer) handleAddressFrozen(l *logEntry, blockNum uint64, txHash, logIdx string) {
	if len(l.Topics) < 4 {
		return
	}
	user := topicAddr(l.Topics[1])
	isFrozen := topicBool(l.Topics[2])
	owner := topicAddr(l.Topics[3])
	id := entityID(l.Address, txHash, logIdx)

	idx.store.SetEntity("TransferAgentAction", id, map[string]interface{}{
		"id":          id,
		"kind":        "AddressFrozen",
		"token":       l.Address,
		"user":        user,
		"isFrozen":    isFrozen,
		"owner":       owner,
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	})
	// Live snapshot
	snapID := fmt.Sprintf("%s:%s", l.Address, user)
	idx.store.SetEntity("FrozenAccount", snapID, map[string]interface{}{
		"id":       snapID,
		"token":    l.Address,
		"user":     user,
		"frozen":   isFrozen,
		"updated":  blockNum,
	})
}

// TokensFrozen / TokensUnfrozen(address indexed user, uint256 amount)
func (idx *Indexer) handleTokensFrozen(l *logEntry, blockNum uint64, txHash, logIdx string, freezing bool) {
	if len(l.Topics) < 2 {
		return
	}
	user := topicAddr(l.Topics[1])
	amount := decodeUint256(l.Data, 0)
	id := entityID(l.Address, txHash, logIdx)
	kind := "TokensFrozen"
	if !freezing {
		kind = "TokensUnfrozen"
	}

	idx.store.SetEntity("TransferAgentAction", id, map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"token":       l.Address,
		"user":        user,
		"amount":      amount.String(),
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	})
	snapID := fmt.Sprintf("%s:%s", l.Address, user)
	// We don't track the running total without a read-modify cycle; just record the latest delta.
	idx.store.SetEntity("FrozenTokens", snapID, map[string]interface{}{
		"id":      snapID,
		"token":   l.Address,
		"user":    user,
		"delta":   amount.String(),
		"kind":    kind,
		"updated": blockNum,
	})
}

// Paused/Unpaused(address user)
func (idx *Indexer) handleSecurityPause(l *logEntry, blockNum uint64, txHash, logIdx string, paused bool) {
	user := decodeAddress(l.Data, 0)
	id := entityID(l.Address, txHash, logIdx)
	kind := "Paused"
	if !paused {
		kind = "Unpaused"
	}
	idx.store.SetEntity("TransferAgentAction", id, map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"token":       l.Address,
		"user":        user,
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	})
}

// RecoverySuccess(address indexed lostWallet, address indexed newWallet, address indexed investorOnchainID)
func (idx *Indexer) handleRecoverySuccess(l *logEntry, blockNum uint64, txHash, logIdx string) {
	if len(l.Topics) < 4 {
		return
	}
	id := entityID(l.Address, txHash, logIdx)
	idx.store.SetEntity("TransferAgentAction", id, map[string]interface{}{
		"id":                id,
		"kind":              "RecoverySuccess",
		"token":             l.Address,
		"lostWallet":        topicAddr(l.Topics[1]),
		"newWallet":         topicAddr(l.Topics[2]),
		"investorOnchainID": topicAddr(l.Topics[3]),
		"blockNumber":       blockNum,
		"txHash":            txHash,
		"logIndex":          logIdx,
	})
}

// ── IdentityRegistry / IdentityRegistryStorage ──────────────────────────────

// IdentityRegistered(address indexed user, IIdentity indexed identity)
// IdentityRemoved(address indexed user, IIdentity indexed identity)
// IdentityUpdated(IIdentity indexed oldIdentity, IIdentity indexed newIdentity)
// IdentityStored(address indexed user, IIdentity indexed identity)
// CountryUpdated(address indexed user, uint16 indexed country)
func (idx *Indexer) handleIdentityRegistryAction(l *logEntry, blockNum uint64, txHash, logIdx, kind string) {
	id := entityID(l.Address, txHash, logIdx)
	entity := map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"registry":    l.Address,
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	}
	if len(l.Topics) >= 2 {
		entity["topic1"] = l.Topics[1]
	}
	if len(l.Topics) >= 3 {
		entity["topic2"] = l.Topics[2]
	}
	idx.store.SetEntity("IdentityRegistryAction", id, entity)
}

// ── ONCHAINID ────────────────────────────────────────────────────────────────

// ClaimAdded/Removed/Changed(bytes32 indexed claimId, uint256 indexed topic, uint256 scheme, address indexed issuer, bytes signature, bytes data, string uri)
func (idx *Indexer) handleClaim(l *logEntry, blockNum uint64, txHash, logIdx, kind string) {
	if len(l.Topics) < 4 {
		return
	}
	id := entityID(l.Address, txHash, logIdx)
	idx.store.SetEntity("OnchainIdClaim", id, map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"identity":    l.Address,
		"claimId":     l.Topics[1],
		"topic":       decodeUint256Topic(l.Topics[2]).String(),
		"issuer":      topicAddr(l.Topics[3]),
		"data":        l.Data,
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	})
}

// KeyAdded/Removed(bytes32 indexed key, uint256 indexed purpose, uint256 indexed keyType)
func (idx *Indexer) handleKey(l *logEntry, blockNum uint64, txHash, logIdx, kind string) {
	if len(l.Topics) < 4 {
		return
	}
	id := entityID(l.Address, txHash, logIdx)
	idx.store.SetEntity("OnchainIdKey", id, map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"identity":    l.Address,
		"key":         l.Topics[1],
		"purpose":     decodeUint256Topic(l.Topics[2]).String(),
		"keyType":     decodeUint256Topic(l.Topics[3]).String(),
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	})
}

// ── TrustedIssuersRegistry / ClaimTopicsRegistry ────────────────────────────

func (idx *Indexer) handleTrustedIssuer(l *logEntry, blockNum uint64, txHash, logIdx, kind string) {
	id := entityID(l.Address, txHash, logIdx)
	entity := map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"registry":    l.Address,
		"data":        l.Data,
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	}
	if len(l.Topics) >= 2 {
		entity["issuer"] = topicAddr(l.Topics[1])
	}
	idx.store.SetEntity("TrustedIssuerAction", id, entity)
}

func (idx *Indexer) handleClaimTopic(l *logEntry, blockNum uint64, txHash, logIdx, kind string) {
	id := entityID(l.Address, txHash, logIdx)
	entity := map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"registry":    l.Address,
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	}
	if len(l.Topics) >= 2 {
		entity["topic"] = decodeUint256Topic(l.Topics[1]).String()
	}
	idx.store.SetEntity("ClaimTopicAction", id, entity)
}

// ── ModularCompliance / IModule ─────────────────────────────────────────────

func (idx *Indexer) handleComplianceEvent(l *logEntry, blockNum uint64, txHash, logIdx, topic0 string) {
	id := entityID(l.Address, txHash, logIdx)
	kind := "ComplianceEvent"
	switch topic0 {
	case SigModuleAdded:
		kind = "ModuleAdded"
	case SigModuleRemoved:
		kind = "ModuleRemoved"
	case SigTokenBound:
		kind = "TokenBound"
	case SigTokenUnbound:
		kind = "TokenUnbound"
	case SigModuleInteraction:
		kind = "ModuleInteraction"
	}
	entity := map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"compliance":  l.Address,
		"data":        l.Data,
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	}
	if len(l.Topics) >= 2 {
		entity["target"] = topicAddr(l.Topics[1])
	}
	idx.store.SetEntity("ComplianceAction", id, entity)
}

// ── Catch-all for events we record but don't decode in detail ───────────────

func (idx *Indexer) handleSimpleSecuritiesEvent(l *logEntry, blockNum uint64, txHash, logIdx, kind string) {
	id := entityID(l.Address, txHash, logIdx)
	idx.store.SetEntity("SecuritiesEvent", id, map[string]interface{}{
		"id":          id,
		"kind":        kind,
		"contract":    l.Address,
		"topics":      l.Topics,
		"data":        l.Data,
		"blockNumber": blockNum,
		"txHash":      txHash,
		"logIndex":    logIdx,
	})
}
