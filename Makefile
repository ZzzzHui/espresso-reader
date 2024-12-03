env:
	@echo export CGO_CFLAGS=\"$(CGO_CFLAGS)\"
	@echo export CGO_LDFLAGS=\"$(CGO_LDFLAGS)\"
	@echo export CARTESI_LOG_LEVEL="debug"
	@echo export CARTESI_BLOCKCHAIN_HTTP_ENDPOINT=""
	@echo export CARTESI_BLOCKCHAIN_WS_ENDPOINT=""
	@echo export CARTESI_BLOCKCHAIN_ID="11155111"
	@echo export CARTESI_CONTRACTS_INPUT_BOX_ADDRESS="0x593E5BCf894D6829Dd26D0810DA7F064406aebB6"
	@echo export CARTESI_CONTRACTS_INPUT_BOX_DEPLOYMENT_BLOCK_NUMBER="6850934"
	@echo export CARTESI_AUTH_MNEMONIC=\"test test test test test test test test test test test junk\"
	@echo export CARTESI_POSTGRES_ENDPOINT="postgres://postgres:password@localhost:5432/rollupsdb?sslmode=disable"
	@echo export CARTESI_TEST_POSTGRES_ENDPOINT="postgres://test_user:password@localhost:5432/test_rollupsdb?sslmode=disable"
	@echo export CARTESI_TEST_MACHINE_IMAGES_PATH=\"$(CARTESI_TEST_MACHINE_IMAGES_PATH)\"
	@echo export PATH=$(CURDIR):$$PATH
	@echo export ESPRESSO_BASE_URL="https://query.decaf.testnet.espresso.network/v0"
	@echo export ESPRESSO_STARTING_BLOCK="882494"
	@echo export ESPRESSO_NAMESPACE="55555"
	@echo export MAIN_SEQUENCER="espresso" # set to either espresso or ethereum
