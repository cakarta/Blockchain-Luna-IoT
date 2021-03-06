package blockchain

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/boltdb/bolt"

	"wizeBlock/wizeNode/core/crypto"
)

const dbFile = "files/db%s/wizebit.db"
const blocksBucket = "blocks"
const genesisCoinbaseData = "The Times 22/Jan/2017 Now I have nice gopher"
const emissionValue = 1000000

// Blockchain implements interactions with a DB
type Blockchain struct {
	tip []byte
	Db  *bolt.DB
}

// Iterator returns a BlockchainIterat
func (bc *Blockchain) Iterator() *BlockchainIterator {
	bci := &BlockchainIterator{bc.tip, bc.Db}

	return bci
}

// CreateBlockchain creates a new blockchain DB
func CreateBlockchain(address, nodeID string) *Blockchain {
	dbFile := fmt.Sprintf(dbFile, nodeID)
	ok, err := DbExists(dbFile)
	if ok {
		fmt.Println("Blockchain already exists.")
		os.Exit(1)
	}

	var tip []byte

	cbtx := NewEmissionCoinbaseTX(address, genesisCoinbaseData, emissionValue)
	genesis := NewGenesisBlock(cbtx)

	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucket([]byte(blocksBucket))
		if err != nil {
			log.Panic(err)
		}

		err = b.Put(genesis.Hash, genesis.Serialize())
		if err != nil {
			log.Panic(err)
		}

		err = b.Put([]byte("l"), genesis.Hash)
		if err != nil {
			log.Panic(err)
		}
		tip = genesis.Hash

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	bc := Blockchain{tip, db}

	return &bc
}

// NewBlockchain creates a new Blockchain with genesis Block
func NewBlockchain(nodeID string) *Blockchain {
	dbFile := fmt.Sprintf(dbFile, nodeID)
	//fmt.Printf("dbFile: %s\n", dbFile)
	ok, err := DbExists(dbFile)
	if !ok {
		fmt.Println("No existing blockchain found. Create one first.")
		os.Exit(1)
	}

	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}

	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		tip = b.Get([]byte("l"))

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	bc := Blockchain{tip, db}
	//fmt.Println("B db:", db, "bc:", bc)

	return &bc
}

// AddBlock saves the block into the blockchain
func (bc *Blockchain) AddBlock(block *Block) {
	err := bc.Db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		blockInDb := b.Get(block.Hash)

		if blockInDb != nil {
			return nil
		}

		blockData := block.Serialize()
		err := b.Put(block.Hash, blockData)
		if err != nil {
			log.Panic(err)
		}

		lastHash := b.Get([]byte("l"))
		lastBlockData := b.Get(lastHash)
		lastBlock := DeserializeBlock(lastBlockData)

		if block.Height > lastBlock.Height {
			err = b.Put([]byte("l"), block.Hash)
			if err != nil {
				log.Panic(err)
			}
			bc.tip = block.Hash
		}

		return nil
	})
	if err != nil {
		log.Panic(err)
	}
}

// FindTransaction finds a transaction by its ID
func (bc *Blockchain) FindTransaction(ID []byte) (Transaction, error) {
	bci := bc.Iterator()

	for {
		block := bci.Next()

		for _, tx := range block.Transactions {
			//fmt.Println("ID: ", ID, " tx.ID: ", tx.ID) //TODO: remove
			if bytes.Compare(tx.ID, ID) == 0 {
				return *tx, nil
			}
		}

		if len(block.PrevBlockHash) == 0 {
			break
		}
	}

	return Transaction{}, errors.New("Transaction is not found")
}

// FindUTXO finds all unspent transaction outputs and returns transactions with spent outputs removed
func (bc *Blockchain) FindUTXO() map[string]TXOutputs {
	UTXO := make(map[string]TXOutputs)
	spentTXOs := make(map[string][]int)
	bci := bc.Iterator()

	for {
		block := bci.Next()

		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)

		Outputs:
			for outIdx, out := range tx.Vout {
				// Was the output spent?
				if spentTXOs[txID] != nil {
					for _, spentOutIdx := range spentTXOs[txID] {
						if spentOutIdx == outIdx {
							continue Outputs
						}
					}
				}

				outs := UTXO[txID]
				outs.Outputs = append(outs.Outputs, out)
				UTXO[txID] = outs
			}

			if tx.IsCoinbase() == false {
				for _, in := range tx.Vin {
					inTxID := hex.EncodeToString(in.Txid)
					spentTXOs[inTxID] = append(spentTXOs[inTxID], in.Vout)
				}
			}
		}

		if len(block.PrevBlockHash) == 0 {
			break
		}
	}

	return UTXO
}

// GetBestHeight returns the height of the latest block
func (bc *Blockchain) GetBestHeight() int {
	var lastBlock Block
	err := bc.Db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash := b.Get([]byte("l"))
		blockData := b.Get(lastHash)
		lastBlock = *DeserializeBlock(blockData)
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	return lastBlock.Height
}

// GetBlock finds a block by its hash and returns it
func (bc *Blockchain) GetBlock(blockHash []byte) (Block, error) {
	var block Block
	err := bc.Db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		blockData := b.Get(blockHash)
		if blockData == nil {
			return errors.New("Block is not found.")
		}
		block = *DeserializeBlock(blockData)
		return nil
	})
	if err != nil {
		return block, err
	}
	return block, nil
}

// GetBlockHashes returns a list of hashes of all the blocks in the chain
func (bc *Blockchain) GetBlockHashes() [][]byte {
	var blocks [][]byte
	bci := bc.Iterator()
	for {
		block := bci.Next()
		blocks = append(blocks, block.Hash)
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return blocks
}

// MineBlock mines a new block with the provided transactions
func (bc *Blockchain) MineBlock(transactions []*Transaction) *Block {
	var lastHash []byte
	var lastHeight int

	for _, tx := range transactions {
		// TODO: ignore transaction if it's not valid
		check, err := bc.VerifyTransaction(tx)
		if err != nil || !check {
			fmt.Printf("ERROR: Invalid transaction: %v\n", err)
			return nil
		}
	}

	err := bc.Db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash = b.Get([]byte("l"))

		blockData := b.Get(lastHash)
		block := DeserializeBlock(blockData)
		lastHeight = block.Height
		return nil
	})
	if err != nil {
		fmt.Printf("ERROR: db.View %v\n", err)
		return nil
		//log.Panic(err)
	}

	newBlock := NewBlock(transactions, lastHash, lastHeight+1)

	if newBlock == nil {
		fmt.Printf("ERROR: NewBlock returns nil")
		return nil
	}

	err = bc.Db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		err := b.Put(newBlock.Hash, newBlock.Serialize())
		if err != nil {
			log.Panic(err)
		}

		err = b.Put([]byte("l"), newBlock.Hash)
		if err != nil {
			fmt.Printf("ERROR: b.Put %v\n", err)
			return nil
			//log.Panic(err)
		}

		bc.tip = newBlock.Hash

		return nil
	})
	if err != nil {
		fmt.Printf("ERROR: db.Update %v\n", err)
		return nil
		//log.Panic(err)
	}
	return newBlock
}

// SignTransaction signs inputs of a Transaction
func (bc *Blockchain) PrepareTransactionToSign(tx *Transaction) (*TransactionToSign, error) {
	prevTXs := make(map[string]Transaction)

	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransaction(vin.Txid)
		if err != nil {
			fmt.Printf("Cant find transaction: %s\n", err)
			return nil, fmt.Errorf("Cant find transaction: %s\n", err)
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}

	return tx.PrepareToSign(prevTXs)
}

func (bc *Blockchain) SignPreparedTransaction(preparedTx *Transaction, txSignatures *TransactionWithSignatures) error {
	prevTXs := make(map[string]Transaction)

	for _, vin := range preparedTx.Vin {
		prevTX, err := bc.FindTransaction(vin.Txid)
		if err != nil {
			fmt.Printf("ERROR: Cant find transaction: %s\n", err)
			return fmt.Errorf("Cant find transaction: %s\n", err)
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}

	return preparedTx.SignPrepared(txSignatures, prevTXs)
}

func (bc *Blockchain) SignTransaction(tx *Transaction, privKey crypto.PrivateKey) {
	prevTXs := make(map[string]Transaction)

	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransaction(vin.Txid)
		if err != nil {
			log.Panic(err)
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}

	tx.Sign(privKey, prevTXs)
}

// VerifyTransaction verifies transaction input signatures
func (bc *Blockchain) VerifyTransaction(tx *Transaction) (bool, error) {
	if tx.IsCoinbase() {
		return true, nil
	}
	prevTXs := make(map[string]Transaction)

	for _, vin := range tx.Vin {
		prevTX, err := bc.FindTransaction(vin.Txid)
		if err != nil {
			return false, err
		}
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}

	return tx.Verify(prevTXs)
}

func (bc *Blockchain) GetBalance(address string) int {
	UTXOSet := UTXOSet{bc}
	balance := 0
	pubKeyHash := crypto.Base58Decode([]byte(address))
	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-4]
	UTXOs := UTXOSet.FindUTXO(pubKeyHash)

	for _, out := range UTXOs {
		balance += out.Value
	}
	return balance
}

func (bc *Blockchain) GetAddresses() []string {
	var addressesMap map[string]int // value will be balance in the future
	addressesMap = make(map[string]int)

	bci := bc.Iterator()

	for {
		block := bci.Next()

		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)
			txID8 := txID[:8]

			for outIdx, out := range tx.Vout {
				fmt.Printf("block: %3d, tx: %s.., outIdx : %d, address: %s, value: %9d\n", block.Height, txID8, outIdx, out.Address, out.Value)
				addressesMap[out.Address] = out.Value
			}

			if tx.IsCoinbase() == false {
				for inIdx, in := range tx.Vin {
					// get outIdx, out from in.Txid, in.Vout
					// find transaction in.Txid
					tx, err := bc.FindTransaction(in.Txid)
					if err != nil {
						fmt.Printf("ERROR: %s", err)
					} else {
						// get transaction output in.Vout
						fmt.Printf("block: %3d, tx: %s.., inIdx  : %d, address: %s, value: %9d\n", block.Height, txID8, inIdx, tx.Vout[in.Vout].Address, tx.Vout[in.Vout].Value)
						addressesMap[tx.Vout[in.Vout].Address] = tx.Vout[in.Vout].Value
					}

				}
			}
		}

		if len(block.PrevBlockHash) == 0 {
			break
		}
	}

	var addressesSlice []string
	addressesSlice = make([]string, 1)
	for address, _ := range addressesMap {
		addressesSlice = append(addressesSlice, address)
	}

	return addressesSlice
}

func (bc *Blockchain) GetWalletBalance(address string) int {
	if !crypto.ValidateAddress(address) {
		log.Panic("ERROR: Address is not valid")
	}

	balance := 0
	pubKeyHash := crypto.Base58Decode([]byte(address))
	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-4]

	UTXOSet := UTXOSet{bc}
	UTXOs := UTXOSet.FindUTXO(pubKeyHash)

	for _, out := range UTXOs {
		balance += out.Value
	}
	return balance
}
