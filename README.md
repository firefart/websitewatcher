# websitewatcher

This tool can be used to monitor websites for changes and trigger an email with a diff to the previous version if they differ.

It also supports extracting only a particular content from the website via regex and capture groups and also to replace content based on a regex (for example to patch out CSRF tokens before comparing).

See the `config.json.sample` file for all possible configuration options.

## Usage

```text
./websitewatcher -config config.json
```

Currently [https://www.diffchecker.com/](https://www.diffchecker.com/) API is used for creating the diff so be sure to thank them for their free service 😎

## Config Options

| Option | Description |
|---|---|
| mail.server | Mailserver to use |
| mail.port | port of the mailserver |
| mail.from.name | the from name on sent emails |
| mail.from.mail | the from email address on sent emails |
| mail.to | array of global receipients. these addresses are included on every watch |
| mail.skiptls | no TLS certificate checks on connecting to mailserver |
| mail.user | smtp username, empty on no authentication |
| mail.password | smtp password |
| timeout | timeout for http requests |
| parallel_checks | number of parallel checks of watches |
| retry.count | number of retries on http errors |
| retry.delay | time to sleep between retries |
| database | filename of the database |
| http_errors_to_ignore | http status codes that should be ignored on all watches |
| useragent | useragent header to use for outgoing http requests |
| watches.name | friendly name of the watch |
| watches.url | the url to check |
| watches.additional_to | array of additional emails for this watch. The email will be sent to the global ones and this list |
| watches.addtional_http_errors_to_ignore | additional http errors to ignore for this watch. The global option is merged with this one |
| watches.header | additional http headers to add |
| watches.disabled | used to disable a watch |
| watches.pattern | the pattern is a regex and must contain one match group. The group is used as the body. This is used to extract the relevant body in big html sites. If left empty the whole body is used |
| watches.replaces.pattern | regex pattern to match in the body |
| watches.replaces.replace_with | replacement string for the regex match |
| watches.retry_on_match | retry request up to retry.count if the response body matches the provided regex |

## Example

In this example we will monitor https://go.dev/dl for new versions.

As we are only interested in the latest version, we use the global `pattern` to extract the content we want. To play with the regexes head over to [https://regex101.com/](https://regex101.com/) and select `go` on the left hand side. Also check the needed modifiers like g, m, s and so on. To include the modifiers in the regex you can prepend it like `(?s)`. Also be sure to escape your regex in the JSON (double quotes and backslashes).

After the body is extracted we clean up the content by removing the content we are not interested part by part. The last 2 `replace` sections remove trailing and leading spaces and double newlines.

The resulting content (see below) is then checked against the last stored version every time the binary runs. To test your config you can run `./websitewatcher -config config.json -debug -test` which will print out the results after each replace so it's easier to debug faulty regexes.

```json
{
  "mail": {
    "server": "in-v3.mailjet.coml",
    "port": 587,
    "from": {
      "name": "websitewatcher",
      "mail": "websitewatcher@mydomain.com"
    },
    "to": ["email@example.com"],
    "skiptls": false,
    "user": "user",
    "password": "pass"
  },
  "timeout": "60s",
  "retries": 1,
  "parallel_checks": 5,
  "database": "db.db",
  "useragent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/102.0.5005.63 Safari/537.36 Edg/102.0.1245.33",
  "watches": [{
    "name": "Golang Downloads",
    "url": "https://go.dev/dl",
    "additional_to": ["person@example.com"],
    "pattern": "(?s)<table class=\"downloadtable\">(.+?)</table>",
    "replaces": [{
        "pattern": "(?s)<thead>.+?</thead>",
        "replace_with": ""
      },
      {
        "pattern": "(?s)<th.*?>.+?</th>",
        "replace_with": ""
      },
      {
        "pattern": "(?s)<td>(Source|Archive|Installer|\\d+MB|Linux|Windows|macOS|FreeBSD|ARMv6|ARM64|ppc64le|x86|x86-64|s390x)</td>",
        "replace_with": ""
      },
      {
        "pattern": "<td.*?>",
        "replace_with": ""
      },
      {
        "pattern": "</td>",
        "replace_with": ""
      },
      {
        "pattern": "<tr.*?>",
        "replace_with": ""
      },
      {
        "pattern": "</tr>",
        "replace_with": ""
      },
      {
        "pattern": "<tt>",
        "replace_with": ""
      },
      {
        "pattern": "</tt>",
        "replace_with": ""
      },
      {
        "pattern": "<a class=\"download\" href=\".+?\">",
        "replace_with": ""
      },
      {
        "pattern": "</a>",
        "replace_with": ""
      },
      {
        "pattern": "(?m)^[\\s\\p{Zs}]+|[\\s\\p{Zs}]+$",
        "replace_with": "\n"
      },
      {
        "pattern": "(?s)\\n\\s*\\n",
        "replace_with": "\n"
      }
    ]
  }]
}
```

This would produce (as of go version 1.20) the following cleaned up output:

```text
go1.20.src.tar.gz
3a29ff0421beaf6329292b8a46311c9fbf06c800077ceddef5fb7f8d5b1ace33
go1.20.darwin-amd64.tar.gz
777025500f62d14bb5a4923072cd97431887961d24de08433a60c2fe1120531d
go1.20.darwin-amd64.pkg
650748a8785ececab2161abd3b5d7b036c021111c6dbaaaee982f28a1b699eb4
go1.20.darwin-arm64.tar.gz
32864d6fe888714ca7b421b5997269c7f6349d7e2675c3a399133e521787608b
go1.20.darwin-arm64.pkg
ca64e724e5a5a60f16a1201d7db2b626a5653c9ac93a3567e8676903c97fd1ef
go1.20.linux-386.tar.gz
1420582fb43a15dbe94760fdd92171315414c4afc21ffe9d3b5875f9386ebe53
go1.20.linux-amd64.tar.gz
5a9ebcc65c1cce56e0d2dc616aff4c4cedcfbda8cc6f0288cc08cda3b18dcbf1
go1.20.linux-arm64.tar.gz
17700b6e5108e2a2c3b1a43cd865d3f9c66b7f1c5f0cec26d3672cc131cc0994
go1.20.linux-armv6l.tar.gz
ee8550213c62812f90dbfd3d098195adedd450379fd4d3bb2c85607fd5a2d283
go1.20.windows-386.zip
9c303e312391eb04b4a1bab9b93b0839e05313068293c26b3a65ec6d24be99ce
go1.20.windows-386.msi
37d7279cd68817c416661280c5daabe8298cf76c631e38aaebe9d1efeaf4257b
go1.20.windows-amd64.zip
e8f6d8bbcf3df58d2ba29818e13b04c2e42ba2e4d90d580720b21c34d10bbf68
go1.20.windows-amd64.msi
179ec1b55d3c1b014595a72fc5f7f59d7c00f9732cc227b47dfe13e6cc633c7c
go1.20.freebsd-386.tar.gz
2f3c68213fa785d0ebfa4e50de5ea8f4baf5d9c12f5783c59e1ee370e35755ae
go1.20.freebsd-amd64.tar.gz
8c5ccff790dda019e070a6a13745aba0c1ea0e3d47076bacf9fb1e0b34cc731f
go1.20.linux-ppc64le.tar.gz
bccbf89c83e0aab2911e57217159bf0fc49bb07c6eebd2c23ae30af18fc5368b
go1.20.linux-s390x.tar.gz
4460deffbc01fe5f31fe226d296e366c0d6059b280743aea49bf81ab62ab8be8
go1.20.windows-arm64.zip
2421b2ade9b68517f962f0ea4fb27b68b5321b334fb1b353de25be5b2ee90cba
go1.20.windows-arm64.msi
3b520f5ef57fb8e0032eeeec5da1665644daa6499234412e91ab1eb44b05881a
```
