# tgsender 

An application for verifying if a phone number is registered on Telegram and sending messages to those users.

# Installation

```sh
go install github.com/soluchok/tgsender@latest 
```

# Check phones
```sh
tgsender check --app-id 2***9 --app-hash c8***e2 --auth 380***70 -p 067***70,068***22 -o users.out
```

# Send message
```sh
tgsender send --app-id 2***9 --app-hash c8***e2 --auth 380***70 --input users.out -m 'Hello there!'
```

# Dump contacts
```sh
tgsender dump --app-id 2***9 --app-hash c8***e2 --auth 380***70 -o dump.out
```
