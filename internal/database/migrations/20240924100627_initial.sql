-- +goose Up
-- +goose StatementBegin
CREATE TABLE watches
(
    id           INTEGER NOT NULL PRIMARY KEY,
    name         TEXT    NOT NULL,
    url          TEXT    NOT NULL,
    last_fetch   DATETIME,
    last_content BLOB
);
CREATE UNIQUE INDEX idx_name_url ON watches (name, url);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_name_url;
DROP TABLE watches;
-- +goose StatementEnd
