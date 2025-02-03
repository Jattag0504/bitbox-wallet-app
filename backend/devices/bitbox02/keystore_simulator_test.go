// Copyright 2025 Shift Crypto AG
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the Liceense.

//go:build bitbox02_simulator
// +build bitbox02_simulator

package bitbox02

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/addresses"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/blockchain"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/maketx"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/types"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	event "github.com/BitBoxSwiss/bitbox-wallet-app/backend/devices/device/event"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/logging"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/socksproxy"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/test"
	"github.com/BitBoxSwiss/bitbox02-api-go/api/common"
	"github.com/BitBoxSwiss/bitbox02-api-go/api/firmware"
	"github.com/BitBoxSwiss/bitbox02-api-go/api/firmware/messages"
	"github.com/BitBoxSwiss/bitbox02-api-go/api/firmware/mocks"
	"github.com/BitBoxSwiss/bitbox02-api-go/communication/u2fhid"
	"github.com/BitBoxSwiss/bitbox02-api-go/util/semver"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/stretchr/testify/require"
)

func mustKeypath(keypath string) signing.AbsoluteKeypath {
	kp, err := signing.NewAbsoluteKeypath(keypath)
	if err != nil {
		panic(err)
	}
	return kp
}

func mustOutpoint(s string) *wire.OutPoint {
	outPoint, err := wire.NewOutPointFromString(s)
	if err != nil {
		panic(err)
	}
	return outPoint
}

func runSimulator(filename string) (func() error, *Device, error) {
	cmd := exec.Command(filename)
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	var conn net.Conn
	var err error
	for range 200 {
		conn, err = net.Dial("tcp", "localhost:15423")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		return nil, nil, err
	}
	const bitboxCMD = 0x80 + 0x40 + 0x01

	communication := u2fhid.NewCommunication(conn, bitboxCMD)
	match := regexp.MustCompile(`v([0-9]+\.[0-9]+\.[0-9]+)`).FindStringSubmatch(filename)
	if len(match) != 2 {
		return nil, nil, errp.New("could not find simulator firmware version")
	}
	version, err := semver.NewSemVerFromString(match[1])
	if err != nil {
		return nil, nil, err
	}
	device := NewDevice("ID", version, common.ProductBitBox02Multi,
		&mocks.Config{}, communication,
	)
	return func() error {
		if err := conn.Close(); err != nil {
			return err
		}
		return cmd.Process.Kill()
	}, device, nil
}

// Download BitBox simulators based on testdata/simulators.json to testdata/simulators/*.
// Skips the download if the file already exists and has the corect hash.
func downloadSimulators() ([]string, error) {
	type simulator struct {
		URL    string `json:"url"`
		Sha256 string `json:"sha256"`
	}
	data, err := os.ReadFile("./testdata/simulators.json")
	if err != nil {
		return nil, err
	}
	var simulators []simulator
	if err := json.Unmarshal(data, &simulators); err != nil {
		return nil, err
	}

	fileNotExistOrHashMismatch := func(filename, expectedHash string) (bool, error) {
		file, err := os.Open(filename)
		if os.IsNotExist(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		defer file.Close()

		hasher := sha256.New()
		if _, err := io.Copy(hasher, file); err != nil {
			return false, err
		}
		actualHash := hex.EncodeToString(hasher.Sum(nil))

		return actualHash != expectedHash, nil
	}

	downloadFile := func(url, filename string) error {
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bad status: %s", resp.Status)
		}

		// Create the file
		out, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, resp.Body)
		return err
	}
	filenames := []string{}
	for _, simulator := range simulators {
		simUrl, err := url.Parse(simulator.URL)
		if err != nil {
			return nil, err
		}
		filename := filepath.Join("testdata", "simulators", path.Base(simUrl.Path))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			return nil, err
		}
		doDownload, err := fileNotExistOrHashMismatch(filename, simulator.Sha256)
		if err != nil {
			return nil, err
		}
		if doDownload {
			if err := downloadFile(simulator.URL, filename); err != nil {
				return nil, err
			}
			if err := os.Chmod(filename, 0755); err != nil {
				return nil, err
			}
		}
		filenames = append(filenames, filename)
	}
	return filenames, nil
}

var downloadSimulatorsOnce = sync.OnceValues(downloadSimulators)

// Runs tests against a simulator which is not initialized (not paired, not seeded).
func testSimulators(t *testing.T, run func(*testing.T, *Device)) {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("Skipping simulator tests: not running on linux-amd64")
	}

	var simulatorFilenames []string
	envSimulator := os.Getenv("SIMULATOR")
	if envSimulator != "" {
		simulatorFilenames = []string{envSimulator}
	} else {
		var err error
		simulatorFilenames, err = downloadSimulatorsOnce()
		require.NoError(t, err)
	}

	for _, simulatorFilename := range simulatorFilenames {
		t.Run(filepath.Base(simulatorFilename), func(t *testing.T) {
			teardown, device, err := runSimulator(simulatorFilename)
			require.NoError(t, err)
			defer func() { require.NoError(t, teardown()) }()
			run(t, device)
		})
	}
}

// Runs tests against a simulator which is not initialized, but paired (not seeded).
func testSimulatorsAfterPairing(t *testing.T, run func(*testing.T, *Device)) {
	t.Helper()
	testSimulators(t, func(t *testing.T, device *Device) {
		t.Helper()
		paired := make(chan struct{})
		device.SetOnEvent(func(ev event.Event, obj interface{}) {
			if ev == event.Event(firmware.EventChannelHashChanged) {
				_, deviceVerified := device.ChannelHash()
				if deviceVerified {
					// Accept pairing.
					device.ChannelHashVerify(true)
					// Unblock
					close(paired)
				}
			}
		})
		require.NoError(t, device.Init(true))
		select {
		case <-paired:
		case <-time.After(15 * time.Second):
			require.Fail(t, "pairing timed out")
		}
		run(t, device)
	})
}

// Runs tests againt a simulator that is seeded with this mnemonic: boring mistake dish oyster truth
// pigeon viable emerge sort crash wire portion cannon couple enact box walk height pull today solid
// off enable tide
func testInitializedSimulators(t *testing.T, run func(*testing.T, *Device)) {
	t.Helper()
	testSimulatorsAfterPairing(t, func(t *testing.T, device *Device) {
		t.Helper()
		require.NoError(t, device.RestoreFromMnemonic())
		run(t, device)
	})
}

func TestSimulatorRootFingerprint(t *testing.T) {
	testInitializedSimulators(t, func(t *testing.T, device *Device) {
		t.Helper()
		fp, err := device.Keystore().RootFingerprint()
		require.NoError(t, err)
		require.Equal(t, "4c00739d", hex.EncodeToString(fp))
	})
}

func TestSimulatorExtendedPublicKeyBTC(t *testing.T) {
	testInitializedSimulators(t, func(t *testing.T, device *Device) {
		t.Helper()
		dbFolder := test.TstTempDir("btc-dbfolder")
		defer func() { _ = os.RemoveAll(dbFolder) }()
		coin := btc.NewCoin(coinpkg.CodeTBTC, "Bitcoin Testnet", "TBTC", coinpkg.BtcUnitDefault, &chaincfg.TestNet3Params, dbFolder, nil, "", socksproxy.NewSocksProxy(false, ""))
		keypath := mustKeypath("m/84'/1'/0'")
		xpub, err := device.Keystore().ExtendedPublicKey(coin, keypath)
		require.NoError(t, err)
		require.Equal(t,
			"xpub6CAkM5q77qFTdrsoqguwTxAnnPVRd4hyHntZaYr9FTcefWi3AaTevG1YTvWzkNuqtshjQnJxpw1YjKLtuQvfvDiDiLVx2XYKZW5baGsRUuC",
			xpub.String(),
		)
	})
}

func TestSimulatorSignBTCTransactionMixedInputs(t *testing.T) {
	testInitializedSimulators(t, func(t *testing.T, device *Device) {
		t.Helper()
		log := logging.Get().WithGroup("simulator tx sign test")
		net := &chaincfg.MainNetParams
		dbFolder := test.TstTempDir("btc-dbfolder")
		defer func() { _ = os.RemoveAll(dbFolder) }()
		coin := btc.NewCoin(coinpkg.CodeBTC, "Bitcoin", "BTC", coinpkg.BtcUnitDefault, net, dbFolder, nil, "", socksproxy.NewSocksProxy(false, ""))

		makeConfig := func(scriptType signing.ScriptType, keypath signing.AbsoluteKeypath) *signing.Configuration {
			xpubStr, err := device.BTCXPub(messages.BTCCoin_BTC, keypath.ToUInt32(), messages.BTCPubRequest_XPUB, false)
			require.NoError(t, err)
			xpub, err := hdkeychain.NewKeyFromString(xpubStr)
			require.NoError(t, err)
			rootFingerprint := []byte{1, 2, 3, 4}
			return signing.NewBitcoinConfiguration(scriptType, rootFingerprint, keypath, xpub)
		}

		configurations := []*signing.Configuration{
			makeConfig(signing.ScriptTypeP2TR, mustKeypath("m/86'/0'/0'")),
			makeConfig(signing.ScriptTypeP2WPKH, mustKeypath("m/84'/0'/0'")),
			makeConfig(signing.ScriptTypeP2WPKHP2SH, mustKeypath("m/49'/0'/0'")),
		}
		inputAddress0 := addresses.NewAccountAddress(configurations[0], signing.NewEmptyRelativeKeypath().Child(0, false).Child(0, false), net, log)
		inputAddress1 := addresses.NewAccountAddress(configurations[1], signing.NewEmptyRelativeKeypath().Child(0, false).Child(0, false), net, log)
		inputAddress2 := addresses.NewAccountAddress(configurations[2], signing.NewEmptyRelativeKeypath().Child(0, false).Child(0, false), net, log)
		changeAddress := addresses.NewAccountAddress(configurations[0], signing.NewEmptyRelativeKeypath().Child(1, false).Child(0, false), net, log)

		prevTx := &wire.MsgTx{
			Version: 2,
			TxIn: []*wire.TxIn{
				{
					PreviousOutPoint: *mustOutpoint("3131313131313131313131313131313131313131313131313131313131313131:0"),
					Sequence:         0xFFFFFFFF,
				},
			},
			TxOut: []*wire.TxOut{
				{
					Value: 100_000_000,
					PkScript: func() []byte {
						return inputAddress0.PubkeyScript()

					}(),
				},
				{
					Value: 100_000_000,
					PkScript: func() []byte {
						return inputAddress1.PubkeyScript()
					}(),
				},
				{
					Value: 100_000_000,
					PkScript: func() []byte {
						return inputAddress2.PubkeyScript()
					}(),
				},
			},
			LockTime: 0,
		}
		prevTxHash := prevTx.TxHash()

		addrs := []*addresses.AccountAddress{
			inputAddress0,
			inputAddress1,
			inputAddress2,
			changeAddress,
		}

		outputPkScript := addresses.NewAccountAddress(configurations[0], signing.NewEmptyRelativeKeypath().Child(0, false).Child(10, false), net, log).PubkeyScript()

		spendableOutputs := map[wire.OutPoint]maketx.UTXO{
			*wire.NewOutPoint(&prevTxHash, 0): maketx.UTXO{prevTx.TxOut[0], inputAddress0},
			*wire.NewOutPoint(&prevTxHash, 1): maketx.UTXO{prevTx.TxOut[1], inputAddress1},
			*wire.NewOutPoint(&prevTxHash, 2): maketx.UTXO{prevTx.TxOut[2], inputAddress2},
		}
		outputAmount := int64(250_000_000)
		feePerKb := btcutil.Amount(1000)
		txProposal, err := maketx.NewTx(
			coin,
			spendableOutputs,
			maketx.NewOutputInfo(outputPkScript),
			outputAmount,
			feePerKb,
			changeAddress,
			log,
		)
		require.NoError(t, err)

		proposedTransaction := &btc.ProposedTransaction{
			TXProposal:                   txProposal,
			AccountSigningConfigurations: configurations,
			GetAccountAddress: func(scriptHashHex blockchain.ScriptHashHex) *addresses.AccountAddress {
				for _, address := range addrs {
					if address.PubkeyScriptHashHex() == scriptHashHex {
						return address
					}
				}
				return nil
			},
			GetPrevTx: func(chainhash.Hash) (*wire.MsgTx, error) {
				return prevTx, nil
			},
			Signatures: make([]*types.Signature, len(txProposal.Transaction.TxIn)),
			FormatUnit: coinpkg.BtcUnitDefault,
		}

		require.NoError(t, device.Keystore().SignTransaction(proposedTransaction))
		require.NoError(t, proposedTransaction.Finalize())
		require.NoError(t, btc.TxValidityCheck(txProposal.Transaction, txProposal.PreviousOutputs, txProposal.SigHashes()))
	})
}
