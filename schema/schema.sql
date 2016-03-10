CREATE TABLE poll (
 id SERIAL PRIMARY KEY,
 name text NOT NULL,
 is_open boolean,
 created_at timestamp
);

CREATE TABLE choices (
 id SERIAL PRIMARY KEY,
 poll_id bigint REFERENCES poll (id),
 answer text NOT NULL,
 created_at timestamp
);

CREATE TABLE answers (
 id SERIAL PRIMARY KEY,
 choice_id bigint REFERENCES choices (id),
 created_at timestamp
)
