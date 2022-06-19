# websitewatcher

This tool can be used to monitor websites for changes and trigger an email with a diff to the previous version if they differ.

It also supports extracting only a particular content from the website via regex and capture groups and also to replace content based on a regex (for example to patch out CSRF tokens before comparing).

See the `config.json.sample` file for all possible configuration options.

Usage:

```
./websitewatcher -config config.json
```

Currently [https://www.diffchecker.com/](https://www.diffchecker.com/) API is used for creating the diff so be sure to thank them for their free service 😎
