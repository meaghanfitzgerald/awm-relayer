package tests

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	relayerEvm "github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/interfaces"
	teleporter_block_hash "github.com/ava-labs/teleporter/abis/go/teleporter-block-hash"
	deploymentUtils "github.com/ava-labs/teleporter/contract-deployment/utils"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"

	. "github.com/onsi/gomega"
)

func PublishBlockHashes() {
	var (
		relayerCmd                *exec.Cmd
		relayerCancel             context.CancelFunc
		blockHashReceiverAddressB common.Address
		subnetAHashes             []common.Hash
		blockHashABI              *abi.ABI
	)

	subnetAInfo := teleporterTestUtils.GetSubnetATestInfo()
	subnetBInfo := teleporterTestUtils.GetSubnetBTestInfo()
	fundedAddress, fundedKey := teleporterTestUtils.GetFundedAccountInfo()

	//
	// Deploy block hash receiver on Subnet B
	//
	ctx := context.Background()

	blockHashReceiverByteCode := testUtils.ReadHexTextFile("./tests/utils/BlockHashReceiverByteCode.txt")

	nonceB, err := subnetBInfo.ChainWSClient.NonceAt(ctx, fundedAddress, nil)
	Expect(err).Should(BeNil())

	blockHashABI, err = teleporter_block_hash.TeleporterBlockHashMetaData.GetAbi()
	Expect(err).Should(BeNil())
	blockHashReceiverAddressB, err = deploymentUtils.DeriveEVMContractAddress(fundedAddress, nonceB)
	Expect(err).Should(BeNil())

	cmdOutput := make(chan string)
	cmd := exec.Command(
		"cast",
		"send",
		"--rpc-url", teleporterTestUtils.HttpToRPCURI(subnetBInfo.ChainNodeURIs[0], subnetBInfo.BlockchainID.String()),
		"--private-key", hexutil.Encode(fundedKey.D.Bytes()),
		"--create", blockHashReceiverByteCode,
	)

	// Set up a pipe to capture the command's output
	cmdReader, err := cmd.StdoutPipe()
	Expect(err).Should(BeNil())
	cmdStdErrReader, err := cmd.StderrPipe()
	Expect(err).Should(BeNil())

	// Start a goroutine to read and output the command's stdout
	go func() {
		scanner := bufio.NewScanner(cmdReader)
		for scanner.Scan() {
			log.Info(scanner.Text())
		}
		cmdOutput <- "Command execution finished"
	}()
	go func() {
		scanner := bufio.NewScanner(cmdStdErrReader)
		for scanner.Scan() {
			log.Error(scanner.Text())
		}
		cmdOutput <- "Command execution finished"
	}()

	err = cmd.Run()
	Expect(err).Should(BeNil())

	// Confirm successful deployment
	deployedCode, err := subnetBInfo.ChainWSClient.CodeAt(ctx, blockHashReceiverAddressB, nil)
	Expect(err).Should(BeNil())
	Expect(len(deployedCode)).Should(BeNumerically(">", 2)) // 0x is an EOA, contract returns the bytecode

	log.Info("Deployed block hash receiver contract", "address", blockHashReceiverAddressB.Hex())

	//
	// Setup relayer config
	//
	hostA, portA, err := teleporterTestUtils.GetURIHostAndPort(subnetAInfo.ChainNodeURIs[0])
	Expect(err).Should(BeNil())

	hostB, portB, err := teleporterTestUtils.GetURIHostAndPort(subnetBInfo.ChainNodeURIs[0])
	Expect(err).Should(BeNil())

	log.Info(
		"Setting up relayer config",
		"hostA", hostA,
		"portA", portA,
		"blockChainA", subnetAInfo.BlockchainID.String(),
		"hostB", hostB,
		"portB", portB,
		"blockChainB", subnetBInfo.BlockchainID.String(),
		"subnetA", subnetAInfo.SubnetID.String(),
		"subnetB", subnetBInfo.SubnetID.String(),
	)

	relayerConfig := config.Config{
		LogLevel:          logging.Info.LowerString(),
		NetworkID:         peers.LocalNetworkID,
		PChainAPIURL:      subnetAInfo.ChainNodeURIs[0],
		EncryptConnection: false,
		StorageLocation:   storageLocation,
		SourceSubnets: []config.SourceSubnet{
			{
				SubnetID:          subnetAInfo.SubnetID.String(),
				ChainID:           subnetAInfo.BlockchainID.String(),
				VM:                config.EVM_BLOCKHASH.String(),
				EncryptConnection: false,
				APINodeHost:       hostA,
				APINodePort:       portA,
				MessageContracts: map[string]config.MessageProtocolConfig{
					"0x0000000000000000000000000000000000000000": {
						MessageFormat: config.BLOCK_HASH_PUBLISHER.String(),
						Settings: map[string]interface{}{
							"destination-chains": []struct {
								ChainID  string `json:"chain-id"`
								Address  string `json:"address"`
								Interval string `json:"interval"`
							}{
								{
									ChainID:  subnetBInfo.BlockchainID.String(),
									Address:  blockHashReceiverAddressB.String(),
									Interval: "5",
								},
							},
						},
					},
				},
			},
		},
		DestinationSubnets: []config.DestinationSubnet{
			{
				SubnetID:          subnetBInfo.SubnetID.String(),
				ChainID:           subnetBInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostB,
				APINodePort:       portB,
				AccountPrivateKey: hex.EncodeToString(fundedKey.D.Bytes()),
			},
		},
	}

	data, err := json.MarshalIndent(relayerConfig, "", "\t")
	Expect(err).Should(BeNil())

	f, err := os.CreateTemp(os.TempDir(), "relayer-config.json")
	Expect(err).Should(BeNil())

	_, err = f.Write(data)
	Expect(err).Should(BeNil())
	relayerConfigPath := f.Name()

	log.Info("Created awm-relayer config", "configPath", relayerConfigPath, "config", string(data))

	//
	// Build Relayer
	//
	cmd = exec.Command("./scripts/build.sh")
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	Expect(err).Should(BeNil())

	//
	// Publish block hashes
	//
	relayerCmd, relayerCancel = testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

	destinationAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")
	gasTipCapA, err := subnetAInfo.ChainWSClient.SuggestGasTipCap(context.Background())
	Expect(err).Should(BeNil())

	baseFeeA, err := subnetAInfo.ChainWSClient.EstimateBaseFee(context.Background())
	Expect(err).Should(BeNil())
	gasFeeCapA := baseFeeA.Mul(baseFeeA, big.NewInt(relayerEvm.BaseFeeFactor))
	gasFeeCapA.Add(gasFeeCapA, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

	// Subscribe to the destination chain block published
	newHeadsB := make(chan *types.Header, 10)
	subB, err := subnetBInfo.ChainWSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer subB.Unsubscribe()

	// Send 5 transactions to produce 5 blocks on subnet A
	// We expect exactly one of the block hashes to be published by the relayer
	for i := 0; i < 5; i++ {
		nonceA, err := subnetAInfo.ChainWSClient.NonceAt(ctx, fundedAddress, nil)
		Expect(err).Should(BeNil())
		value := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(1)) // 1eth
		txA := types.NewTx(&types.DynamicFeeTx{
			ChainID:   subnetAInfo.ChainIDInt,
			Nonce:     nonceA,
			To:        &destinationAddress,
			Gas:       teleporterTestUtils.DefaultTeleporterTransactionGas,
			GasFeeCap: gasFeeCapA,
			GasTipCap: gasTipCapA,
			Value:     value,
		})
		txSignerA := types.LatestSignerForChainID(subnetAInfo.ChainIDInt)

		triggerTxA, err := types.SignTx(txA, txSignerA, fundedKey)
		Expect(err).Should(BeNil())

		receipt := teleporterTestUtils.SendTransactionAndWaitForAcceptance(ctx, subnetAInfo.ChainWSClient, triggerTxA)

		log.Info("Sent block on destination", "blockHash", receipt.BlockHash)
		subnetAHashes = append(subnetAHashes, receipt.BlockHash)
	}

	// Listen on the destination chain for the published  block hash
	newHeadB := <-newHeadsB
	log.Info("Fetching log from the newly produced block")

	blockHashB := newHeadB.Hash()

	logs, err := subnetBInfo.ChainWSClient.FilterLogs(ctx, interfaces.FilterQuery{
		BlockHash: &blockHashB,
		Addresses: []common.Address{blockHashReceiverAddressB},
		Topics: [][]common.Hash{
			{
				blockHashABI.Events["ReceiveBlockHash"].ID,
			},
		},
	})
	Expect(err).Should(BeNil())

	bind, err := teleporter_block_hash.NewTeleporterBlockHash(blockHashReceiverAddressB, subnetBInfo.ChainWSClient)
	Expect(err).Should(BeNil())
	event, err := bind.ParseReceiveBlockHash(logs[0])
	Expect(err).Should(BeNil())

	// The published block hash should match one of the ones sent on Subnet A
	foundHash := false
	for _, blockHash := range subnetAHashes {
		if hex.EncodeToString(blockHash[:]) == hex.EncodeToString(event.BlockHash[:]) {
			foundHash = true
			break
		}
	}
	if !foundHash {
		Expect(false).Should(BeTrue(), "published block hash does not match any of the sent block hashes")
	}
	log.Info("Received published block hash on destination", "blockHash", hex.EncodeToString(event.BlockHash[:]))

	// We shouldn't receive any more blocks, since the relayer is configured to publish once every 5 blocks on the source
	log.Info("Waiting for 10s to ensure no new block confirmations on destination chain")
	Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())

	// Cancel the command and stop the relayer
	relayerCancel()
	_ = relayerCmd.Wait()
}
