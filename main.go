package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/covalenthq/covalent-api-sdk-go/chains"
	"github.com/covalenthq/covalent-api-sdk-go/covalentclient"
	"github.com/covalenthq/covalent-api-sdk-go/services"
)

// Структура для хранения данных о паре
type PairData struct {
	ChainID     string `json:"chainId"`
	DexID       string `json:"dexId"`
	URL         string `json:"url"`
	PairAddress string `json:"pairAddress"`
	BaseToken   struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
	} `json:"baseToken"`
	QuoteToken struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
	} `json:"quoteToken"`
	PriceNative string `json:"priceNative"`
	PriceUSD    string `json:"priceUsd"`
	Volume      struct {
		M5  float64 `json:"m5"`
		H1  float64 `json:"h1"`
		H6  float64 `json:"h6"`
		H24 float64 `json:"h24"`
	} `json:"volume"`
	Liquidity struct {
		USD float64 `json:"usd"`
	} `json:"liquidity"`
	Txns struct {
		M5 struct {
			Buys  int `json:"buys"`
			Sells int `json:"sells"`
		} `json:"m5"`
		H1 struct {
			Buys  int `json:"buys"`
			Sells int `json:"sells"`
		} `json:"h1"`
		H6 struct {
			Buys  int `json:"buys"`
			Sells int `json:"sells"`
		} `json:"h6"`
		H24 struct {
			Buys  int `json:"buys"`
			Sells int `json:"sells"`
		} `json:"h24"`
	} `json:"txns"`
	PriceChange struct {
		M5  float64 `json:"m5"`
		H1  float64 `json:"h1"`
		H6  float64 `json:"h6"`
		H24 float64 `json:"h24"`
	} `json:"priceChange"`
	CreatedAt int64 `json:"pairCreatedAt"`
	Boosts    struct {
		Active int `json:"active"`
	} `json:"boosts"`
}

// Структура для хранения данных о транзакции
type TransactionData struct {
	Date           string  `json:"date"`
	AmountUSD      float64 `json:"amount_usd"`
	TokenCountQuote float64 `json:"token_count_quote"`
	TokenCountBase float64 `json:"token_count_base"`
	Price          float64 `json:"price"`
	Wallet         string  `json:"wallet"`
	Type           string  `json:"type"`
}

// Функция для получения данных о паре по адресу контракта
func getPairData(contractAddress string) (*PairData, error) {
	url := fmt.Sprintf("https://api.dexscreener.com/latest/dex/search/?q=%s", contractAddress)
	fmt.Println("Fetching data from:", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pair data: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	fmt.Println("API Response:", string(body))

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("token not found (404). Please check the contract address")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned non-200 status code: %d. Response: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Pairs []PairData `json:"pairs"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Pairs) == 0 {
		return nil, fmt.Errorf("no pairs found for the given contract address")
	}

	// Возвращаем первую пару
	return &response.Pairs[0], nil
}

// Функция для получения данных о транзакциях за последние 7 дней
func getRecentTransactions(apiKey, chainID, contractAddress string, baseTokenSymbol string, quoteTokenSymbol string) ([]TransactionData, error) {
	client := covalentclient.CovalentClient(apiKey)
	if client == nil || client.TransactionService == nil {
		return nil, fmt.Errorf("failed to create client or transaction service")
	}

	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -7)

	var transactions []TransactionData

	transactionChannel := client.TransactionService.GetAllTransactionsForAddress(chains.Chain(chainID), contractAddress, services.GetAllTransactionsForAddressQueryParamOpts{})

	var firstTransactionResponse string
	firstTransactionFound := false

	for txResult := range transactionChannel {
		if txResult.Err != nil {
			fmt.Printf("Error fetching transactions: %s\n", txResult.Err)
			return nil, fmt.Errorf("failed to fetch transactions: %w", txResult.Err)
		}
		tx := txResult.Transaction

		if !firstTransactionFound {
			txJSON, _ := json.MarshalIndent(tx, "", "  ")
			firstTransactionResponse = string(txJSON)
			fmt.Printf("First Transaction API Response: %s\n", firstTransactionResponse)
			firstTransactionFound = true
		}

		if tx.BlockSignedAt == nil {
			fmt.Println("Skipping transaction with nil BlockSignedAt")
			continue
		}

		if tx.BlockSignedAt.Before(startDate) {
			break
		}

		fmt.Printf("Processing transaction with BlockSignedAt: %s\n", tx.BlockSignedAt.String())

		if tx.Value == nil || tx.ValueQuote == nil || tx.FromAddress == nil {
			fmt.Println("Skipping transaction with nil values")
			continue
		}

		value, _ := tx.Value.Float64()
		// Пропускаем пустые транзакции
		if value == 0 || math.IsNaN(value) {
			fmt.Println("Skipping transaction with zero or NaN value")
			continue
		}

		transactionType := "unknown"
		var tokenCountBase float64
		if tx.LogEvents != nil {
			for _, log := range *tx.LogEvents {
				if log.Decoded != nil && log.Decoded.Name != nil && (*log.Decoded.Name == "Swap" || *log.Decoded.Name == "execute") {
					for _, param := range *log.Decoded.Params {
						if param.Value != nil {
							if paramValue, ok := (*param.Value).(string); ok {
								if param.Name != nil && *param.Name == "buyer" && paramValue == *tx.FromAddress {
									transactionType = "buy"
								} else if param.Name != nil && *param.Name == "seller" && paramValue == *tx.FromAddress {
									transactionType = "sell"
								}
							}
							// Извлекаем количество базовых токенов
							if param.Name != nil && (*param.Name == "amount0Out" || *param.Name == "amount1Out" || *param.Name == "value") {
								tokenCountBase, _ = strconv.ParseFloat(fmt.Sprintf("%v", *param.Value), 64)
								tokenCountBase = tokenCountBase / math.Pow(10, 18) // Преобразуем в токены
							}
						}
					}
				}
			}
		}

		// Делим количество токенов на 10^18 чтобы получить значение в токенах
		tokenCountQuote := value / math.Pow(10, 18)

		transactions = append(transactions, TransactionData{
			Date:           tx.BlockSignedAt.Format("2006-01-02 15:04:05"), // Изменено форматирование даты
			AmountUSD:      *tx.ValueQuote,
			TokenCountQuote: tokenCountQuote,
			TokenCountBase: tokenCountBase,
			Price:          *tx.ValueQuote / tokenCountQuote,
			Wallet:         *tx.FromAddress,
			Type:           transactionType,
		})
	}

	return transactions, nil
}

// Функция для сохранения отчёта в файл
func writeReportToFile(filename string, pairData *PairData, transactions []TransactionData) error {
	// Добавляем расширение .txt, если оно отсутствует
	if len(filename) < 4 || filename[len(filename)-4:] != ".txt" {
		filename += ".txt"
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Время создания отчёта
	reportTime := time.Now().Format(time.RFC3339)

	report := fmt.Sprintf("Report Created At: %s\n\n", reportTime)
	report += fmt.Sprintf("Pair Address: %s\n", pairData.PairAddress)
	report += fmt.Sprintf("DEX: %s (%s)\n", pairData.DexID, pairData.ChainID)
	report += fmt.Sprintf("URL: %s\n", pairData.URL)
	report += fmt.Sprintf("Base Token: %s (%s)\n", pairData.BaseToken.Name, pairData.BaseToken.Symbol)
	report += fmt.Sprintf("Quote Token: %s (%s)\n", pairData.QuoteToken.Name, pairData.QuoteToken.Symbol)
	report += fmt.Sprintf("Price (Native): %s\n", pairData.PriceNative)
	report += fmt.Sprintf("Price (USD): %s\n", pairData.PriceUSD)
	report += fmt.Sprintf("Volume (5m): %.2f\n", pairData.Volume.M5)
	report += fmt.Sprintf("Volume (1h): %.2f\n", pairData.Volume.H1)
	report += fmt.Sprintf("Volume (6h): %.2f\n", pairData.Volume.H6)
	report += fmt.Sprintf("Volume (24h): %.2f\n", pairData.Volume.H24)
	report += fmt.Sprintf("Liquidity (USD): %.2f\n", pairData.Liquidity.USD)
	report += fmt.Sprintf("Transactions (5m): Buys: %d, Sells: %d\n", pairData.Txns.M5.Buys, pairData.Txns.M5.Sells)
	report += fmt.Sprintf("Transactions (1h): Buys: %d, Sells: %d\n", pairData.Txns.H1.Buys, pairData.Txns.H1.Sells)
	report += fmt.Sprintf("Transactions (6h): Buys: %d, Sells: %d\n", pairData.Txns.H6.Buys, pairData.Txns.H6.Sells)
	report += fmt.Sprintf("Transactions (24h): Buys: %d, Sells: %d\n", pairData.Txns.H24.Buys, pairData.Txns.H24.Sells)
	report += fmt.Sprintf("Price Change (5m): %.2f%%\n", pairData.PriceChange.M5)
	report += fmt.Sprintf("Price Change (1h): %.2f%%\n", pairData.PriceChange.H1)
	report += fmt.Sprintf("Price Change (6h): %.2f%%\n", pairData.PriceChange.H6)
	report += fmt.Sprintf("Price Change (24h): %.2f%%\n", pairData.PriceChange.H24)
	report += fmt.Sprintf("Boosts (Active): %d\n", pairData.Boosts.Active)
	report += fmt.Sprintf("Pair Created At: %s\n", time.Unix(pairData.CreatedAt/1000, 0).Format(time.RFC3339))

	report += "\nRecent Transactions (Last 7 days):\n"
	for _, tx := range transactions {
		tokenCountQuoteFormatted := fmt.Sprintf("%.5f", tx.TokenCountQuote)
		tokenCountQuoteFormatted = strings.ReplaceAll(tokenCountQuoteFormatted, ".", ",")
		tokenCountBaseFormatted := fmt.Sprintf("%.5f", tx.TokenCountBase)
		tokenCountBaseFormatted = strings.ReplaceAll(tokenCountBaseFormatted, ".", ",")
		priceFormatted := fmt.Sprintf("$%.7f", tx.Price)
		report += fmt.Sprintf("Date: %s, Amount (USD): %.2f, Token Count (%s): %s, Token Count (%s): %s, Price: %s, Wallet: %s, Type: %s\n",
			tx.Date, tx.AmountUSD, pairData.QuoteToken.Symbol, tokenCountQuoteFormatted, pairData.BaseToken.Symbol, tokenCountBaseFormatted, priceFormatted, tx.Wallet, tx.Type)
	}

	_, err = file.WriteString(report)
	if err != nil {
		return fmt.Errorf("failed to write report to file: %w", err)
	}

	return nil
}

func main() {
	myApp := app.New()
	myWindow := myApp.NewWindow("Crypto Token Analyzer")

	// Устанавливаем тему (можно использовать встроенные темы или кастомные)
	myApp.Settings().SetTheme(theme.LightTheme())

	// Поле для ввода адреса контракта с иконкой
	contractAddressEntry := widget.NewEntry()
	contractAddressEntry.SetPlaceHolder("Enter contract address...")

	// Поле для ввода имени файла с иконкой
	filenameEntry := widget.NewEntry()
	filenameEntry.SetPlaceHolder("Enter filename...")

	// Поле для ввода API ключа
	apiKeyEntry := widget.NewEntry()
	apiKeyEntry.SetPlaceHolder("Enter Covalent API key...")

	// Поле для ввода ChainID
	chainIDEntry := widget.NewSelect([]string{
		"eth-mainnet",
		"bsc-mainnet",
		"matic-mainnet",
		"avalanche-mainnet",
		"fantom-mainnet",
		"optimism-mainnet",
		"arbitrum-mainnet",
		"solana-mainnet",
		"celo-mainnet",
		"moonbeam-mainnet",
	}, func(value string) {})

	// Метка для отображения статуса
	statusLabel := widget.NewLabel("")

	// Кнопка для запуска анализа
	analyzeButton := widget.NewButtonWithIcon("Analyze", theme.DocumentCreateIcon(), func() {
		contractAddress := contractAddressEntry.Text
		filename := filenameEntry.Text
		apiKey := apiKeyEntry.Text
		chainID := chainIDEntry.Selected

		if contractAddress == "" || filename == "" || apiKey == "" || chainID == "" {
			dialog.ShowInformation("Error", "Please fill all fields", myWindow)
			return
		}

		statusLabel.SetText("Fetching pair data...")
		pairData, err := getPairData(contractAddress)
		if err != nil {
			statusLabel.SetText("")
			dialog.ShowError(fmt.Errorf("failed to get pair data: %w", err), myWindow)
			return
		}

		statusLabel.SetText("Fetching recent transactions...")
		transactions, err := getRecentTransactions(apiKey, chainID, contractAddress, pairData.BaseToken.Symbol, pairData.QuoteToken.Symbol)
		if err != nil {
			statusLabel.SetText("")
			dialog.ShowError(fmt.Errorf("failed to get recent transactions: %w", err), myWindow)
			return
		}

		fmt.Printf("Fetched transactions: %+v\n", transactions)

		statusLabel.SetText("Writing report to file...")
		err = writeReportToFile(filename, pairData, transactions)
		if err != nil {
			statusLabel.SetText("")
			dialog.ShowError(fmt.Errorf("failed to write report: %w", err), myWindow)
			return
		}

		statusLabel.SetText("")
		dialog.ShowInformation("Success", "Report successfully written to "+filename, myWindow)
	})

	// Карточка для формы
	formCard := widget.NewCard(
		"Token Analysis",
		"Enter the contract address and filename to generate a report",
		container.NewVBox(
			container.NewVBox(
				widget.NewLabel("Contract Address"),
				contractAddressEntry,
			),
			container.NewVBox(
				widget.NewLabel("Filename"),
				filenameEntry,
			),
			container.NewVBox(
				widget.NewLabel("Covalent API Key"),
				apiKeyEntry,
			),
			container.NewVBox(
				widget.NewLabel("Chain ID"),
				chainIDEntry,
			),
			analyzeButton,
		),
	)

	// Устанавливаем содержимое окна
	myWindow.SetContent(container.NewVBox(
		formCard,
		statusLabel, // Добавляем statusLabel в интерфейс
	))

	// Устанавливаем размер окна
	myWindow.Resize(fyne.NewSize(500, 400))
	myWindow.ShowAndRun()
}
