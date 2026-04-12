//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/storage"
)

// ---------------------------------------------------------------------------
// Governance resolvers — proposals, votes, delegations
// ---------------------------------------------------------------------------

func TestResolver_Governance_Proposal(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Proposal", "prop-1", map[string]interface{}{
		"id": "prop-1", "title": "Increase validator cap", "proposer": "0xabc", "status": "active",
		"forVotes": "1500000", "againstVotes": "200000",
	})

	data := execOK(t, eng, `{ proposal(id: "prop-1") { id title proposer status } }`)
	p := requireMap(t, data, "proposal")
	if p["status"] != "active" {
		t.Errorf("status: got %v", p["status"])
	}
}

func TestResolver_Governance_Proposals(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Proposal", "prop-1", map[string]interface{}{
		"id": "prop-1", "title": "Increase fee", "status": "executed",
	})

	data := execOK(t, eng, `{ proposals(first: 10) { id } }`)
	requireList(t, data, "proposals", 1)
}

func TestResolver_Governance_Vote(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Vote", "vote-1", map[string]interface{}{
		"id": "vote-1", "proposal": "prop-1", "voter": "0xdef", "support": true, "weight": "500000",
	})

	data := execOK(t, eng, `{ vote(id: "vote-1") { id voter support weight } }`)
	v := requireMap(t, data, "vote")
	if v["voter"] != "0xdef" {
		t.Errorf("voter: got %v", v["voter"])
	}
}

func TestResolver_Governance_Votes(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Vote", "vote-1", map[string]interface{}{
		"id": "vote-1", "proposal": "prop-1", "voter": "0xdef",
	})

	data := execOK(t, eng, `{ votes(first: 10) { id } }`)
	requireList(t, data, "votes", 1)
}

func TestResolver_Governance_Delegation(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Delegation", "del-1", map[string]interface{}{
		"id": "del-1", "delegator": "0xaaa", "delegate": "0xbbb", "amount": "100000",
	})

	data := execOK(t, eng, `{ delegation(id: "del-1") { id delegator delegate amount } }`)
	d := requireMap(t, data, "delegation")
	if d["delegator"] != "0xaaa" {
		t.Errorf("delegator: got %v", d["delegator"])
	}
}

func TestResolver_Governance_Delegations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Delegation", "del-1", map[string]interface{}{
		"id": "del-1", "delegator": "0xaaa", "delegate": "0xbbb",
	})

	data := execOK(t, eng, `{ delegations(first: 10) { id } }`)
	requireList(t, data, "delegations", 1)
}

func TestResolver_Governance_MissingID(t *testing.T) {
	eng, _ := newTestEngine(t)
	msg := execErr(t, eng, `{ proposal { id } }`)
	if msg != "proposal requires id" {
		t.Errorf("unexpected error: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// DAO resolvers — proposals, members, treasury
// ---------------------------------------------------------------------------

func TestResolver_DAO_Proposal(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DAOProposal", "dp-1", map[string]interface{}{
		"id": "dp-1", "title": "Fund research", "author": "0xfed", "status": "queued",
	})

	data := execOK(t, eng, `{ daoProposal(id: "dp-1") { id title status } }`)
	p := requireMap(t, data, "daoProposal")
	if p["status"] != "queued" {
		t.Errorf("status: got %v", p["status"])
	}
}

func TestResolver_DAO_Proposals(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DAOProposal", "dp-1", map[string]interface{}{
		"id": "dp-1", "title": "Fund research",
	})

	data := execOK(t, eng, `{ daoProposals(first: 10) { id } }`)
	requireList(t, data, "daoProposals", 1)
}

func TestResolver_DAO_Member(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DAOMember", "dm-1", map[string]interface{}{
		"id": "dm-1", "address": "0x111", "votingPower": "50000", "joinedAt": 1700000000,
	})

	data := execOK(t, eng, `{ daoMember(id: "dm-1") { id address votingPower } }`)
	m := requireMap(t, data, "daoMember")
	if m["address"] != "0x111" {
		t.Errorf("address: got %v", m["address"])
	}
}

func TestResolver_DAO_Members(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DAOMember", "dm-1", map[string]interface{}{"id": "dm-1", "address": "0x111"})

	data := execOK(t, eng, `{ daoMembers(first: 10) { id } }`)
	requireList(t, data, "daoMembers", 1)
}

func TestResolver_DAO_Treasury(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DAOTreasury", "dt-1", map[string]interface{}{
		"id": "dt-1", "balance": "5000000", "token": "LUX",
	})

	data := execOK(t, eng, `{ daoTreasury(id: "dt-1") { id balance token } }`)
	tr := requireMap(t, data, "daoTreasury")
	if tr["token"] != "LUX" {
		t.Errorf("token: got %v", tr["token"])
	}
}

func TestResolver_DAO_Treasuries(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DAOTreasury", "dt-1", map[string]interface{}{"id": "dt-1", "balance": "100"})

	data := execOK(t, eng, `{ daoTreasuries(first: 10) { id } }`)
	requireList(t, data, "daoTreasuries", 1)
}

// ---------------------------------------------------------------------------
// Treasury resolvers — deposits, withdrawals, allocations, budgets
// ---------------------------------------------------------------------------

func TestResolver_Treasury_Deposit(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TreasuryDeposit", "td-1", map[string]interface{}{
		"id": "td-1", "from": "0xabc", "amount": "10000", "token": "LUX", "timestamp": 1700000000,
	})

	data := execOK(t, eng, `{ treasuryDeposit(id: "td-1") { id from amount token } }`)
	d := requireMap(t, data, "treasuryDeposit")
	if d["amount"] != "10000" {
		t.Errorf("amount: got %v", d["amount"])
	}
}

func TestResolver_Treasury_Deposits(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TreasuryDeposit", "td-1", map[string]interface{}{"id": "td-1", "amount": "10000"})

	data := execOK(t, eng, `{ treasuryDeposits(first: 10) { id } }`)
	requireList(t, data, "treasuryDeposits", 1)
}

func TestResolver_Treasury_Withdrawal(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TreasuryWithdrawal", "tw-1", map[string]interface{}{
		"id": "tw-1", "to": "0xdef", "amount": "5000", "reason": "dev grant",
	})

	data := execOK(t, eng, `{ treasuryWithdrawal(id: "tw-1") { id to amount reason } }`)
	w := requireMap(t, data, "treasuryWithdrawal")
	if w["reason"] != "dev grant" {
		t.Errorf("reason: got %v", w["reason"])
	}
}

func TestResolver_Treasury_Withdrawals(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TreasuryWithdrawal", "tw-1", map[string]interface{}{"id": "tw-1", "amount": "5000"})

	data := execOK(t, eng, `{ treasuryWithdrawals(first: 10) { id } }`)
	requireList(t, data, "treasuryWithdrawals", 1)
}

func TestResolver_Treasury_Allocation(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TreasuryAllocation", "ta-1", map[string]interface{}{
		"id": "ta-1", "recipient": "0x999", "amount": "25000", "category": "development",
	})

	data := execOK(t, eng, `{ treasuryAllocation(id: "ta-1") { id recipient amount category } }`)
	a := requireMap(t, data, "treasuryAllocation")
	if a["category"] != "development" {
		t.Errorf("category: got %v", a["category"])
	}
}

func TestResolver_Treasury_Allocations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TreasuryAllocation", "ta-1", map[string]interface{}{"id": "ta-1", "amount": "25000"})

	data := execOK(t, eng, `{ treasuryAllocations(first: 10) { id } }`)
	requireList(t, data, "treasuryAllocations", 1)
}

func TestResolver_Treasury_Budget(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TreasuryBudget", "tb-1", map[string]interface{}{
		"id": "tb-1", "period": "Q1-2026", "total": "500000", "spent": "120000",
	})

	data := execOK(t, eng, `{ treasuryBudget(id: "tb-1") { id period total spent } }`)
	b := requireMap(t, data, "treasuryBudget")
	if b["period"] != "Q1-2026" {
		t.Errorf("period: got %v", b["period"])
	}
}

func TestResolver_Treasury_Budgets(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TreasuryBudget", "tb-1", map[string]interface{}{"id": "tb-1", "total": "500000"})

	data := execOK(t, eng, `{ treasuryBudgets(first: 10) { id } }`)
	requireList(t, data, "treasuryBudgets", 1)
}

// ---------------------------------------------------------------------------
// Liquid staking resolvers — stakes, unstakes, rewards, validators
// ---------------------------------------------------------------------------

func TestResolver_Liquid_Stake(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("LiquidStake", "ls-1", map[string]interface{}{
		"id": "ls-1", "staker": "0xabc", "amount": "100000", "shares": "95000", "validator": "NodeID-abc",
	})

	data := execOK(t, eng, `{ liquidStake(id: "ls-1") { id staker amount shares } }`)
	s := requireMap(t, data, "liquidStake")
	if s["staker"] != "0xabc" {
		t.Errorf("staker: got %v", s["staker"])
	}
}

func TestResolver_Liquid_Stakes(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("LiquidStake", "ls-1", map[string]interface{}{"id": "ls-1", "amount": "100000"})

	data := execOK(t, eng, `{ liquidStakes(first: 10) { id } }`)
	requireList(t, data, "liquidStakes", 1)
}

func TestResolver_Liquid_Unstake(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("LiquidUnstake", "lu-1", map[string]interface{}{
		"id": "lu-1", "staker": "0xdef", "shares": "50000", "amount": "52000", "status": "pending",
	})

	data := execOK(t, eng, `{ liquidUnstake(id: "lu-1") { id staker shares status } }`)
	u := requireMap(t, data, "liquidUnstake")
	if u["status"] != "pending" {
		t.Errorf("status: got %v", u["status"])
	}
}

func TestResolver_Liquid_Unstakes(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("LiquidUnstake", "lu-1", map[string]interface{}{"id": "lu-1", "shares": "50000"})

	data := execOK(t, eng, `{ liquidUnstakes(first: 10) { id } }`)
	requireList(t, data, "liquidUnstakes", 1)
}

func TestResolver_Liquid_Reward(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("LiquidReward", "lr-1", map[string]interface{}{
		"id": "lr-1", "epoch": 42, "amount": "1500", "validator": "NodeID-xyz",
	})

	data := execOK(t, eng, `{ liquidReward(id: "lr-1") { id epoch amount } }`)
	r := requireMap(t, data, "liquidReward")
	if fmt.Sprint(r["epoch"]) != "42" {
		t.Errorf("epoch: got %v", r["epoch"])
	}
}

func TestResolver_Liquid_Rewards(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("LiquidReward", "lr-1", map[string]interface{}{"id": "lr-1", "amount": "1500"})

	data := execOK(t, eng, `{ liquidRewards(first: 10) { id } }`)
	requireList(t, data, "liquidRewards", 1)
}

func TestResolver_Liquid_Validator(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("LiquidValidator", "lv-1", map[string]interface{}{
		"id": "lv-1", "nodeID": "NodeID-abc", "totalStaked": "500000", "commission": "5",
	})

	data := execOK(t, eng, `{ liquidValidator(id: "lv-1") { id nodeID totalStaked commission } }`)
	v := requireMap(t, data, "liquidValidator")
	if v["nodeID"] != "NodeID-abc" {
		t.Errorf("nodeID: got %v", v["nodeID"])
	}
}

func TestResolver_Liquid_Validators(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("LiquidValidator", "lv-1", map[string]interface{}{"id": "lv-1", "nodeID": "NodeID-abc"})

	data := execOK(t, eng, `{ liquidValidators(first: 10) { id } }`)
	requireList(t, data, "liquidValidators", 1)
}

// ---------------------------------------------------------------------------
// DID resolvers — documents, delegates, attributes
// ---------------------------------------------------------------------------

func TestResolver_DID_Document(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DIDDocument", "did-1", map[string]interface{}{
		"id": "did-1", "controller": "0xabc", "created": 1700000000, "updated": 1700100000,
	})

	data := execOK(t, eng, `{ didDocument(id: "did-1") { id controller created } }`)
	d := requireMap(t, data, "didDocument")
	if d["controller"] != "0xabc" {
		t.Errorf("controller: got %v", d["controller"])
	}
}

func TestResolver_DID_Documents(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DIDDocument", "did-1", map[string]interface{}{"id": "did-1", "controller": "0xabc"})

	data := execOK(t, eng, `{ didDocuments(first: 10) { id } }`)
	requireList(t, data, "didDocuments", 1)
}

func TestResolver_DID_Delegate(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DIDDelegate", "dd-1", map[string]interface{}{
		"id": "dd-1", "identity": "did-1", "delegateType": "veriKey", "delegate": "0xdef",
		"validity": 86400,
	})

	data := execOK(t, eng, `{ didDelegate(id: "dd-1") { id identity delegateType delegate } }`)
	d := requireMap(t, data, "didDelegate")
	if d["delegateType"] != "veriKey" {
		t.Errorf("delegateType: got %v", d["delegateType"])
	}
}

func TestResolver_DID_Delegates(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DIDDelegate", "dd-1", map[string]interface{}{"id": "dd-1", "delegate": "0xdef"})

	data := execOK(t, eng, `{ didDelegates(first: 10) { id } }`)
	requireList(t, data, "didDelegates", 1)
}

func TestResolver_DID_Attribute(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DIDAttribute", "da-1", map[string]interface{}{
		"id": "da-1", "identity": "did-1", "name": "did/svc/hub", "value": "https://hub.example.com",
		"validity": 172800,
	})

	data := execOK(t, eng, `{ didAttribute(id: "da-1") { id identity name value } }`)
	a := requireMap(t, data, "didAttribute")
	if a["name"] != "did/svc/hub" {
		t.Errorf("name: got %v", a["name"])
	}
}

func TestResolver_DID_Attributes(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DIDAttribute", "da-1", map[string]interface{}{"id": "da-1", "name": "did/svc/hub"})

	data := execOK(t, eng, `{ didAttributes(first: 10) { id } }`)
	requireList(t, data, "didAttributes", 1)
}

// ---------------------------------------------------------------------------
// Prediction market resolvers — markets, positions, resolutions
// ---------------------------------------------------------------------------

func TestResolver_Prediction_Market(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PredictionMarket", "pm-1", map[string]interface{}{
		"id": "pm-1", "question": "LUX > $10 by Q2?", "creator": "0xabc",
		"totalStaked": "50000", "status": "open", "expiresAt": 1730000000,
	})

	data := execOK(t, eng, `{ predictionMarket(id: "pm-1") { id question status totalStaked } }`)
	m := requireMap(t, data, "predictionMarket")
	if m["status"] != "open" {
		t.Errorf("status: got %v", m["status"])
	}
}

func TestResolver_Prediction_Markets(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PredictionMarket", "pm-1", map[string]interface{}{"id": "pm-1", "status": "open"})

	data := execOK(t, eng, `{ predictionMarkets(first: 10) { id } }`)
	requireList(t, data, "predictionMarkets", 1)
}

func TestResolver_Prediction_Position(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PredictionPosition", "pp-1", map[string]interface{}{
		"id": "pp-1", "market": "pm-1", "trader": "0xdef", "outcome": "yes", "amount": "1000",
	})

	data := execOK(t, eng, `{ predictionPosition(id: "pp-1") { id trader outcome amount } }`)
	p := requireMap(t, data, "predictionPosition")
	if p["outcome"] != "yes" {
		t.Errorf("outcome: got %v", p["outcome"])
	}
}

func TestResolver_Prediction_Positions(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PredictionPosition", "pp-1", map[string]interface{}{"id": "pp-1", "outcome": "yes"})

	data := execOK(t, eng, `{ predictionPositions(first: 10) { id } }`)
	requireList(t, data, "predictionPositions", 1)
}

func TestResolver_Prediction_Resolution(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PredictionResolution", "pr-1", map[string]interface{}{
		"id": "pr-1", "market": "pm-1", "outcome": "yes", "resolver": "0xfed", "timestamp": 1730000000,
	})

	data := execOK(t, eng, `{ predictionResolution(id: "pr-1") { id outcome resolver } }`)
	r := requireMap(t, data, "predictionResolution")
	if r["outcome"] != "yes" {
		t.Errorf("outcome: got %v", r["outcome"])
	}
}

func TestResolver_Prediction_Resolutions(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PredictionResolution", "pr-1", map[string]interface{}{"id": "pr-1", "outcome": "yes"})

	data := execOK(t, eng, `{ predictionResolutions(first: 10) { id } }`)
	requireList(t, data, "predictionResolutions", 1)
}

// ---------------------------------------------------------------------------
// Securities resolvers — issuances, transfers, dividends, compliance
// ---------------------------------------------------------------------------

func TestResolver_Securities_Issuance(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SecurityIssuance", "si-1", map[string]interface{}{
		"id": "si-1", "issuer": "0xabc", "symbol": "LUXS", "totalSupply": "1000000",
		"standard": "ERC-1400",
	})

	data := execOK(t, eng, `{ securityIssuance(id: "si-1") { id issuer symbol totalSupply } }`)
	s := requireMap(t, data, "securityIssuance")
	if s["symbol"] != "LUXS" {
		t.Errorf("symbol: got %v", s["symbol"])
	}
}

func TestResolver_Securities_Issuances(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SecurityIssuance", "si-1", map[string]interface{}{"id": "si-1", "symbol": "LUXS"})

	data := execOK(t, eng, `{ securityIssuances(first: 10) { id } }`)
	requireList(t, data, "securityIssuances", 1)
}

func TestResolver_Securities_Transfer(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SecurityTransfer", "st-1", map[string]interface{}{
		"id": "st-1", "from": "0xaaa", "to": "0xbbb", "amount": "500", "token": "LUXS",
		"partition": "default",
	})

	data := execOK(t, eng, `{ securityTransfer(id: "st-1") { id from to amount } }`)
	tr := requireMap(t, data, "securityTransfer")
	if tr["from"] != "0xaaa" {
		t.Errorf("from: got %v", tr["from"])
	}
}

func TestResolver_Securities_Transfers(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SecurityTransfer", "st-1", map[string]interface{}{"id": "st-1", "amount": "500"})

	data := execOK(t, eng, `{ securityTransfers(first: 10) { id } }`)
	requireList(t, data, "securityTransfers", 1)
}

func TestResolver_Securities_Dividend(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SecurityDividend", "sd-1", map[string]interface{}{
		"id": "sd-1", "token": "LUXS", "amountPerShare": "0.05", "recordDate": 1700000000,
		"paymentDate": 1700100000,
	})

	data := execOK(t, eng, `{ securityDividend(id: "sd-1") { id token amountPerShare } }`)
	d := requireMap(t, data, "securityDividend")
	if d["amountPerShare"] != "0.05" {
		t.Errorf("amountPerShare: got %v", d["amountPerShare"])
	}
}

func TestResolver_Securities_Dividends(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SecurityDividend", "sd-1", map[string]interface{}{"id": "sd-1", "token": "LUXS"})

	data := execOK(t, eng, `{ securityDividends(first: 10) { id } }`)
	requireList(t, data, "securityDividends", 1)
}

func TestResolver_Securities_Compliance(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SecurityCompliance", "sc-1", map[string]interface{}{
		"id": "sc-1", "investor": "0xabc", "kycStatus": "approved", "accredited": true,
		"jurisdiction": "US",
	})

	data := execOK(t, eng, `{ securityCompliance(id: "sc-1") { id investor kycStatus jurisdiction } }`)
	c := requireMap(t, data, "securityCompliance")
	if c["kycStatus"] != "approved" {
		t.Errorf("kycStatus: got %v", c["kycStatus"])
	}
}

func TestResolver_Securities_Compliances(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SecurityCompliance", "sc-1", map[string]interface{}{"id": "sc-1", "kycStatus": "approved"})

	data := execOK(t, eng, `{ securityCompliances(first: 10) { id } }`)
	requireList(t, data, "securityCompliances", 1)
}

// ---------------------------------------------------------------------------
// Derivatives resolvers — contracts, settlements, exercises
// ---------------------------------------------------------------------------

func TestResolver_Derivatives_Contract(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DerivativeContract", "dc-1", map[string]interface{}{
		"id": "dc-1", "underlying": "LUX", "contractType": "future", "strike": "10.00",
		"expiry": 1735000000, "notional": "100000",
	})

	data := execOK(t, eng, `{ derivativeContract(id: "dc-1") { id underlying contractType strike } }`)
	c := requireMap(t, data, "derivativeContract")
	if c["contractType"] != "future" {
		t.Errorf("contractType: got %v", c["contractType"])
	}
}

func TestResolver_Derivatives_Contracts(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DerivativeContract", "dc-1", map[string]interface{}{"id": "dc-1", "contractType": "future"})

	data := execOK(t, eng, `{ derivativeContracts(first: 10) { id } }`)
	requireList(t, data, "derivativeContracts", 1)
}

func TestResolver_Derivatives_Settlement(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DerivativeSettlement", "ds-1", map[string]interface{}{
		"id": "ds-1", "contract": "dc-1", "settledPrice": "11.50", "pnl": "1500",
		"settledAt": 1735000000,
	})

	data := execOK(t, eng, `{ derivativeSettlement(id: "ds-1") { id settledPrice pnl } }`)
	s := requireMap(t, data, "derivativeSettlement")
	if s["settledPrice"] != "11.50" {
		t.Errorf("settledPrice: got %v", s["settledPrice"])
	}
}

func TestResolver_Derivatives_Settlements(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DerivativeSettlement", "ds-1", map[string]interface{}{"id": "ds-1", "pnl": "1500"})

	data := execOK(t, eng, `{ derivativeSettlements(first: 10) { id } }`)
	requireList(t, data, "derivativeSettlements", 1)
}

func TestResolver_Derivatives_Exercise(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DerivativeExercise", "de-1", map[string]interface{}{
		"id": "de-1", "contract": "dc-1", "exerciser": "0xabc", "amount": "50000",
		"exercisedAt": 1734500000,
	})

	data := execOK(t, eng, `{ derivativeExercise(id: "de-1") { id exerciser amount } }`)
	e := requireMap(t, data, "derivativeExercise")
	if e["exerciser"] != "0xabc" {
		t.Errorf("exerciser: got %v", e["exerciser"])
	}
}

func TestResolver_Derivatives_Exercises(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DerivativeExercise", "de-1", map[string]interface{}{"id": "de-1", "amount": "50000"})

	data := execOK(t, eng, `{ derivativeExercises(first: 10) { id } }`)
	requireList(t, data, "derivativeExercises", 1)
}

// ---------------------------------------------------------------------------
// Stats resolvers — verify all 8 stats endpoints resolve without error
// ---------------------------------------------------------------------------

func TestResolver_NewStats_AllNil(t *testing.T) {
	eng, _ := newTestEngine(t)

	statsFields := []struct {
		name  string
		query string
	}{
		{"governanceStats", `{ governanceStats { id } }`},
		{"daoStats", `{ daoStats { id } }`},
		{"treasuryStats", `{ treasuryStats { id } }`},
		{"liquidStats", `{ liquidStats { id } }`},
		{"didStats", `{ didStats { id } }`},
		{"predictionStats", `{ predictionStats { id } }`},
		{"securityStats", `{ securityStats { id } }`},
		{"derivativeStats", `{ derivativeStats { id } }`},
	}

	for _, tt := range statsFields {
		t.Run(tt.name, func(t *testing.T) {
			data := execOK(t, eng, tt.query)
			// nil is fine for unseeded stats
			if data[tt.name] != nil {
				t.Errorf("expected nil for unseeded %s, got %v", tt.name, data[tt.name])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LoadBuiltin — verify each new schema loads by name
// ---------------------------------------------------------------------------

func TestResolver_LoadBuiltin_NewSchemas(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	names := []string{
		"governance", "dao", "treasury", "liquid", "liquid-staking",
		"did", "did-registry", "prediction", "prediction-market",
		"securities", "security-token", "derivatives", "futures", "options",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			eng := engine.New(store, nil)
			if err := eng.LoadBuiltin(name); err != nil {
				t.Fatalf("LoadBuiltin(%q) failed: %v", name, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Missing ID errors — verify all new single-entity resolvers require id
// ---------------------------------------------------------------------------

func TestResolver_NewResolvers_MissingID(t *testing.T) {
	eng, _ := newTestEngine(t)

	cases := []struct {
		query string
		want  string
	}{
		{`{ proposal { id } }`, "proposal requires id"},
		{`{ vote { id } }`, "vote requires id"},
		{`{ delegation { id } }`, "delegation requires id"},
		{`{ daoProposal { id } }`, "daoProposal requires id"},
		{`{ daoMember { id } }`, "daoMember requires id"},
		{`{ daoTreasury { id } }`, "daoTreasury requires id"},
		{`{ treasuryDeposit { id } }`, "treasuryDeposit requires id"},
		{`{ treasuryWithdrawal { id } }`, "treasuryWithdrawal requires id"},
		{`{ treasuryAllocation { id } }`, "treasuryAllocation requires id"},
		{`{ treasuryBudget { id } }`, "treasuryBudget requires id"},
		{`{ liquidStake { id } }`, "liquidStake requires id"},
		{`{ liquidUnstake { id } }`, "liquidUnstake requires id"},
		{`{ liquidReward { id } }`, "liquidReward requires id"},
		{`{ liquidValidator { id } }`, "liquidValidator requires id"},
		{`{ didDocument { id } }`, "didDocument requires id"},
		{`{ didDelegate { id } }`, "didDelegate requires id"},
		{`{ didAttribute { id } }`, "didAttribute requires id"},
		{`{ predictionMarket { id } }`, "predictionMarket requires id"},
		{`{ predictionPosition { id } }`, "predictionPosition requires id"},
		{`{ predictionResolution { id } }`, "predictionResolution requires id"},
		{`{ securityIssuance { id } }`, "securityIssuance requires id"},
		{`{ securityTransfer { id } }`, "securityTransfer requires id"},
		{`{ securityDividend { id } }`, "securityDividend requires id"},
		{`{ securityCompliance { id } }`, "securityCompliance requires id"},
		{`{ derivativeContract { id } }`, "derivativeContract requires id"},
		{`{ derivativeSettlement { id } }`, "derivativeSettlement requires id"},
		{`{ derivativeExercise { id } }`, "derivativeExercise requires id"},
	}

	for _, tt := range cases {
		t.Run(tt.want, func(t *testing.T) {
			msg := execErr(t, eng, tt.query)
			if msg != tt.want {
				t.Errorf("got %q, want %q", msg, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Nonexistent ID — verify new single-entity resolvers return nil, not error
// ---------------------------------------------------------------------------

func TestResolver_NewResolvers_NonexistentID(t *testing.T) {
	eng, _ := newTestEngine(t)

	queries := []struct {
		field string
		query string
	}{
		{"proposal", `{ proposal(id: "nonexistent") { id } }`},
		{"daoProposal", `{ daoProposal(id: "nonexistent") { id } }`},
		{"treasuryDeposit", `{ treasuryDeposit(id: "nonexistent") { id } }`},
		{"liquidStake", `{ liquidStake(id: "nonexistent") { id } }`},
		{"didDocument", `{ didDocument(id: "nonexistent") { id } }`},
		{"predictionMarket", `{ predictionMarket(id: "nonexistent") { id } }`},
		{"securityIssuance", `{ securityIssuance(id: "nonexistent") { id } }`},
		{"derivativeContract", `{ derivativeContract(id: "nonexistent") { id } }`},
	}

	for _, tt := range queries {
		t.Run(tt.field, func(t *testing.T) {
			data := execOK(t, eng, tt.query)
			if data[tt.field] != nil {
				t.Errorf("expected nil for nonexistent %s, got %v", tt.field, data[tt.field])
			}
		})
	}
}
