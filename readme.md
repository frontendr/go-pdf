# PDF Rendering Service

A high-performance PDF generation service that converts web pages and HTML content to PDF files using a headless browser. Built with Go and powered by [go-rod](https://github.com/go-rod/rod).

## Features

- 🚀 Fast PDF generation from URLs or HTML content
- 🎨 Full CSS and JavaScript support
- 📄 Customizable page size, orientation, and margins
- 🔄 Page pooling for efficient resource management
- 🐳 Docker support with easy deployment
- ⚙️ Configurable via environment variables or command-line flags

## Quick Start

### Using Docker (Recommended)

```bash
# Build the image
docker build -t go-pdf .

# Run the container
docker run -d -p 80:80 --name go-pdf go-pdf

# Access the service
curl "http://localhost:80/pdf?url=https://example.com" > output.pdf
```

### Running Locally

```bash
# Install dependencies
go mod download

# Create a .env file (optional)
cp .env.example .env

# Run the service
go run main.go
```

## Usage

### GET Request - Convert a Web Page

Convert an existing web page to PDF by providing its URL:

```bash
curl "http://localhost:80/pdf?url=https://example.com" > output.pdf
```

**Optional Query Parameters:**

- `scale` - Scale of the webpage rendering (default: 1.0)
- `pageRanges` - Paper ranges to print, e.g., '1-5, 8, 11-13'
- `printBackground` - Whether to print background graphics (default: true)
- `landscape` - Paper orientation (default: false)
- `paperWidth` - Paper width in inches (default: 8.27)
- `paperHeight` - Paper height in inches (default: 11.7)
- `displayHeaderFooter` - Whether to display header and footer (default: false)
- `headerTemplate` - HTML template for the print header
- `footerTemplate` - HTML template for the print footer
- `preferCSSPageSize` - Whether to prefer page size as defined by CSS (default: false)

**Example with parameters:**

```bash
curl "http://localhost:80/pdf?url=https://example.com&landscape=true&paperWidth=11&paperHeight=8.5" > output.pdf
```

### POST Request - Convert HTML Content

Convert custom HTML content to PDF by sending the HTML in the request body:

```bash
curl -X POST \
  -H "Content-Type: text/html" \
  -d '<html><body><h1>Hello World</h1><p>This is a PDF generated from HTML content.</p></body></html>' \
  "http://localhost:80/pdf" > output.pdf
```

**Example with parameters:**

```bash
curl -X POST \
  -H "Content-Type: text/html" \
  -d '<html><body><h1>Invoice</h1></body></html>' \
  "http://localhost:80/pdf?paperWidth=8.5&paperHeight=11" > invoice.pdf
```

## Configuration

The service can be configured using environment variables or command-line flags.

| Environment Variable | Command Line Flag | Description | Default |
|---------------------|-------------------|-------------|---------|
| `PDF_ENDPOINT` | `-path` | PDF endpoint or path | `/pdf` |
| `LISTEN_ADDRESS` | `-host` | Listen address | `0.0.0.0:3005` |
| `PROFILING_ENABLED` | `-profiling` | Enable profiling | `false` |
| `PAGE_POOL_SIZE` | `-pool` | Page pool size | `5` |
| `LOG_LEVEL` | `-log` | Log level (debug, info, warn, error, fatal, panic) | `info` |
| `LOG_FILE` | `-log-file` | Log file path | `` |

### Configuration Examples

**Using environment variables:**

```bash
PDF_ENDPOINT=/convert LISTEN_ADDRESS=0.0.0.0:8080 PAGE_POOL_SIZE=10 ./go-pdf
```

**Using command-line flags:**

```bash
./go-pdf -path /convert -host 0.0.0.0:8080 -pool 10
```

**Using a .env file:**

Create a `.env` file in the project root:

```env
LISTEN_ADDRESS=0.0.0.0:3005
PDF_ENDPOINT=/pdf
PROFILING_ENABLED=false
PAGE_POOL_SIZE=5
LOG_LEVEL=info
LOG_FILE=
```

## Docker

### Building the Docker Image

```bash
docker build -t go-pdf .
```

The Dockerfile uses a multi-stage build to create an optimized image:
- Build stage: Compiles the Go application
- Final stage: Ubuntu 22.04 with necessary dependencies for Chromium

**Note:** The image is built for `linux/amd64` platform to ensure compatibility with Chromium. If you're on Apple Silicon (M1/M2/M3), the image will run under emulation.

### Running the Container

**Basic usage:**

```bash
docker run -d -p 80:80 --name go-pdf go-pdf
```

**With custom port:**

```bash
docker run -d -p 8080:80 --name go-pdf go-pdf
```

**With environment variable overrides:**

```bash
docker run -d -p 80:80 \
  -e PAGE_POOL_SIZE=10 \
  -e LOG_LEVEL=debug \
  --name go-pdf \
  go-pdf
```

**With custom configuration:**

The Docker image uses `.env.docker` as the default configuration file, which sets:
- `LISTEN_ADDRESS=0.0.0.0:80`
- `PDF_ENDPOINT=/pdf`
- `LOG_LEVEL=warning`
- `PAGE_POOL_SIZE=5`

You can override any of these values using the `-e` flag when running the container.

### Docker Management Commands

```bash
# View logs
docker logs go-pdf

# Stop the container
docker stop go-pdf

# Start the container
docker start go-pdf

# Restart the container
docker restart go-pdf

# Remove the container
docker rm go-pdf

# Remove the image
docker rmi go-pdf
```

## Examples

### Basic PDF Generation

```bash
curl "http://localhost:80/pdf?url=https://example.com" > example.pdf
```

### Landscape PDF

```bash
curl "http://localhost:80/pdf?url=https://example.com&landscape=true" > landscape.pdf
```

### Custom Page Size (Letter)

```bash
curl "http://localhost:80/pdf?url=https://example.com&paperWidth=8.5&paperHeight=11" > letter.pdf
```

### HTML to PDF

```bash
curl -X POST \
  -H "Content-Type: text/html" \
  -d '<html><body><h1>My Document</h1><p>Content here</p></body></html>' \
  "http://localhost:80/pdf" > document.pdf
```

### Using JavaScript fetch API

```javascript
// GET request
fetch('http://localhost:80/pdf?url=https://example.com')
  .then(response => response.blob())
  .then(blob => {
    const url = URL.createObjectURL(blob);
    window.open(url);
  });

// POST request
fetch('http://localhost:80/pdf?paperWidth=8.5&paperHeight=11', {
  method: 'POST',
  headers: {
    'Content-Type': 'text/html'
  },
  body: '<html><body><h1>Hello World</h1></body></html>'
})
  .then(response => response.blob())
  .then(blob => {
    const url = URL.createObjectURL(blob);
    window.open(url);
  });
```

## Development

### Prerequisites

- Go 1.24 or higher
- Docker (for containerized deployment)

### Building from Source

```bash
# Clone the repository
git clone <repository-url>
cd go-pdf

# Install dependencies
go mod download

# Build the binary
go build -o go-pdf main.go

# Run the service
./go-pdf
```

### Running Tests

```bash
go test ./...
```

## License

MIT

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
