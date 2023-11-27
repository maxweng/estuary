//go:build calibnet
// +build calibnet

package build

import (
	"github.com/filecoin-project/go-address"
)

// Three miners with most power as of 2021-09-17
var calibnetMinerStrs = []string{
	"t033435",
	"t037612",
	"t01433",
	"t033523",
	"t01767",
	"t018209",
	"t036820",
	"t033430",
	"t03112",
	"t03149",
	"t01247",
}

var defaultCalibnetDatabaseValue = "sqlite=estuary_calibnet.db"

func init() {
	SetAddressNetwork(address.Testnet)
	SetDefaultMiners(calibnetMinerStrs)
	SetDefaultDatabaseValue(defaultCalibnetDatabaseValue)
}
