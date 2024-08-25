package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

type MessageEntry struct {
	Amount  string `json:"amount"`
	Address string `json:"address"`
}

func massSender(seedPhrase string, messageEntryFilename string) {
	client := liteclient.NewConnectionPool()

	// get config
	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton.org/global.config.json")
	if err != nil {
		log.Error("get config err:", err.Error())
		return
	}

	// connect to mainnet lite servers
	err = client.AddConnectionsFromConfig(context.Background(), cfg)
	if err != nil {
		log.Error("connection err:", err.Error())
		return
	}

	// api client with full proof checks
	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetry()
	api.SetTrustedBlockFromConfig(cfg)

	words := strings.Split(seedPhrase, " ")

	log.Infof("Seed words: %s", words)

	// Initialize highload wallet
	w, err := wallet.FromSeed(api, words, wallet.V4R2)
	if err != nil {
		log.Error("FromSeed err:", err.Error())
		return
	}

	block, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		log.Error("CurrentMasterchainInfo err:", err.Error())
		return
	}

	balance, err := w.GetBalance(context.Background(), block)
	if err != nil {
		log.Error("GetBalance err:", err.Error())
		return
	}

	// Read message entries from file
	data, err := os.ReadFile(messageEntryFilename)
	if err != nil {
		log.Error("Error reading file:", err.Error())
		return
	}

	var messages []MessageEntry
	err = json.Unmarshal(data, &messages)
	if err != nil {
		log.Error("Error unmarshalling JSON:", err.Error())
		return
	}

	// if balance < len(messages) * 0.05 + their amount then exit
	sum := 0.0
	for _, msg := range messages {
		// from string to float
		amount, err := strconv.ParseFloat(msg.Amount, 64)
		if err != nil {
			log.Error("Error parsing amount:", err.Error())
			return
		}

		sum += amount
	}

	if float64(balance.Nano().Int64()) < (float64(len(messages))*5e7)+sum {
		log.Error("Not enough balance to send all messages")
		return
	}

	// Start sending messages in batches
	const batchSize = 4

	comment, err := wallet.CreateCommentCell("")
	if err != nil {
		log.Fatal("CreateComment err:", err.Error())
		return
	}
	for i := 0; i < len(messages); i += batchSize {
		end := i + batchSize
		if end > len(messages) {
			end = len(messages)
		}

		batch := messages[i:end]
		var walletMessages []*wallet.Message
		for _, msg := range batch {
			addr := address.MustParseAddr(msg.Address)
			walletMessages = append(walletMessages, &wallet.Message{
				Mode: wallet.PayGasSeparately + wallet.IgnoreErrors, // pay fee separately, ignore action errors
				InternalMessage: &tlb.InternalMessage{
					IHRDisabled: true, // disable hyper routing (currently not works in ton)
					Bounce:      addr.IsBounceable(),
					DstAddr:     addr,
					Amount:      tlb.MustFromTON(msg.Amount),
					Body:        comment,
				},
			})
		}

		// send transaction that contains all our messages, and wait for confirmation
		txHash, err := w.SendManyWaitTxHash(context.Background(), walletMessages)
		if err != nil {
			log.Error("Transfer err:", err.Error())
			return
		}

		log.Print("transaction sent, hash:", base64.StdEncoding.EncodeToString(txHash))
		log.Print("explorer link: https://tonscan.org/tx/" + base64.URLEncoding.EncodeToString(txHash))

		log.Info("Sleeping for 30 seconds...")
		time.Sleep(30 * time.Second)
	}
}
