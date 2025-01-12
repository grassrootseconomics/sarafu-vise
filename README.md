# URDT USSD service

This is a USSD service built using the [go-vise](https://github.com/nolash/go-vise) engine.

## Prerequisites
### 1. [go-vise](https://github.com/nolash/go-vise)

Set up `go-vise` by cloning the repository into a separate directory. The main upstream repository is hosted at: `https://git.defalsify.org/vise.git`
```
git clone https://git.defalsify.org/vise.git
```

## Setup
1. Clone the ussd repo in its own directory

    ```
    git clone https://git.grassecon.net/urdt/ussd.git
    ```

2. Navigate to the project directory.
3. Enter the `services/registration` subfolder:
    ```
    cd services/registration
    ```
4. make the .bin files from the .vis files
    ```
    make VISE_PATH=/var/path/to/your/go-vise -B
    ```
5. Return to the project root (`cd ../..`)
6. Run the USSD menu 
    ```
    go run cmd/main.go -session-id=0712345678
    ```
## Running the different binaries
1. ### CLI: 
    ```
    go run cmd/main.go -session-id=0712345678
    ```
2. ### Africastalking: 
    ```
    go run cmd/africastalking/main.go
    ```
3. ### Async: 
    ```
    go run cmd/async/main.go
    ```
4. ### Http: 
    ```
    go run cmd/http/main.go
    ```
    
## Flags
Below are the supported flags:

1. `-session-id`: 
    
    Specifies the session ID. (CLI only). 
    
    Default: `075xx2123`.

    Example:
    ```
    go run cmd/main.go -session-id=0712345678
    ```

2. `-d`: 

    Enables engine debug output. 
    
    Default: `false`.

    Example:
    ```
    go run cmd/main.go -session-id=0712345678 -d
    ```

3. `-db`: 

    Specifies the database type.
    
    Default: `gdbm`.

    Example:
    ```
    go run cmd/main.go -session-id=0712345678 -d -db=postgres
    ```

    >Note: If using `-db=postgres`, ensure PostgreSQL is running with the connection details specified in your `.env` file.

## License

[AGPL-3.0](LICENSE).