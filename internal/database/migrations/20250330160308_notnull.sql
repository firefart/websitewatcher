-- +goose Up
-- +goose StatementBegin
CREATE TEMPORARY TABLE watches_backup
(
    id           INTEGER NOT NULL PRIMARY KEY,
    name         TEXT    NOT NULL,
    url          TEXT    NOT NULL,
    last_fetch   DATETIME,
    last_content BLOB
);
INSERT INTO watches_backup SELECT id, name, url, last_fetch, last_content FROM watches;
DROP TABLE watches;
CREATE TABLE watches
(
    id           INTEGER NOT NULL PRIMARY KEY,
    name         TEXT    NOT NULL,
    url          TEXT    NOT NULL,
    last_fetch   DATETIME NOT NULL,
    last_content BLOB NOT NULL
);
INSERT INTO watches SELECT id, name, url, last_fetch, last_content FROM watches_backup;
DROP TABLE watches_backup;
CREATE UNIQUE INDEX idx_name_url ON watches (name, url);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- +goose StatementEnd
