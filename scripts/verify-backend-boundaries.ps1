$ErrorActionPreference = 'Stop'

function Assert-NoMatches {
	param(
		[string]$Pattern,
		[string]$Path,
		[string]$Message,
		[Parameter(ValueFromRemainingArguments = $true)]
		[string[]]$RgOptions
	)
	& rg -n --glob '*.go' @RgOptions $Pattern $Path
    if ($LASTEXITCODE -eq 1) {
        return
    }
    if ($LASTEXITCODE -eq 0) {
        throw $Message
    }
    throw "rg failed while checking: $Message"
}

# Presentation handlers delegate persistence and transactions to application services.
Assert-NoMatches 'pool\.Query|pool\.QueryRow|pool\.Exec|\.Begin\(|tx\.Query|tx\.QueryRow|tx\.Exec|pgxpool\.Pool|pgx\.Tx' 'internal/api' 'internal/api must not access PostgreSQL directly'
Assert-NoMatches 'SELECT |INSERT INTO|UPDATE |DELETE FROM' 'internal/api' 'internal/api must not contain SQL'

# Domain/application packages must stay independent of the HTTP transport.
Assert-NoMatches 'net/http|http\.ResponseWriter|\*http\.Request|shared_response|pkg_http_request' 'internal/rental' 'internal/rental must not depend on HTTP presentation'
# The payment package currently owns its webhook transport adapter; exclude
# that explicit presentation file while guarding the application/repository
# implementation beneath it.
Assert-NoMatches 'net/http|http\.ResponseWriter|\*http\.Request|shared_response|pkg_http_request' 'internal/payment' 'internal/payment application code must not depend on HTTP presentation' '--glob' '!handler.go' '--glob' '!handler_test.go'
Assert-NoMatches 'net/http|http\.ResponseWriter|\*http\.Request|shared_response|pkg_http_request' 'internal/review' 'internal/review must not depend on HTTP presentation'
Assert-NoMatches 'net/http|http\.ResponseWriter|\*http\.Request|shared_response|pkg_http_request' 'internal/notification' 'internal/notification must not depend on HTTP presentation'

Write-Output 'Backend architecture boundary checks passed.'
