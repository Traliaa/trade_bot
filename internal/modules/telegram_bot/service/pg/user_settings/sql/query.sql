-- name: Insert :one
INSERT INTO user_settings (
    chatid, name, auth_code, step
) VALUES (
             @chatid, @name, @auth_code::text, @step::text
         ) returning id;


-- name: Update :exec
UPDATE user_settings
SET  name = @name, auth_code = @auth_code::text, step = @step::text
WHERE chatid = @chatid;



-- name: Delete :exec
DELETE FROM user_settings
WHERE chatid = @chatid and  id = @id;


-- name: GetById :one
SELECT id, name, auth_code::text, step::text FROM user_settings WHERE chatid = @chatid;


-- name: GetAll :many
SELECT id, chatid, name, auth_code::text, step::text FROM user_settings;