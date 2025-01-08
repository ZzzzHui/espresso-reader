FROM --platform=amd64 debian:latest

ENV CGO_CFLAGS="-I/opt/cartesi/include"
ENV CGO_LDFLAGS="-L/opt/cartesi/lib"
ENV CARTESI_LOG_LEVEL="debug"
ENV CARTESI_BLOCKCHAIN_HTTP_ENDPOINT=
ENV CARTESI_BLOCKCHAIN_WS_ENDPOINT=
ENV CARTESI_BLOCKCHAIN_ID="11155111"
ENV CARTESI_CONTRACTS_INPUT_BOX_ADDRESS="0x593E5BCf894D6829Dd26D0810DA7F064406aebB6"
ENV CARTESI_CONTRACTS_INPUT_BOX_DEPLOYMENT_BLOCK_NUMBER="6850934"
ENV CARTESI_AUTH_MNEMONIC=
ENV CARTESI_POSTGRES_ENDPOINT="postgres://postgres:password@localhost:5432/rollupsdb?sslmode=disable"
ENV CARTESI_TEST_POSTGRES_ENDPOINT="postgres://test_user:password@localhost:5432/test_rollupsdb?sslmode=disable"
ENV CARTESI_TEST_MACHINE_IMAGES_PATH="/opt/cartesi/share/cartesi-machine/images/"
ENV ESPRESSO_BASE_URL="https://query.decaf.testnet.espresso.network"
ENV ESPRESSO_STARTING_BLOCK="977675"
ENV ESPRESSO_NAMESPACE="55555"

COPY . .

RUN <<EOF
dpkg -i ./espresso-reader_1.0.0_amd64.deb
apt-get update
apt-get install -y ca-certificates
EOF
