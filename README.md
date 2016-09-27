# hidden-polls: A simple app polling app.

This doesn't use do anything at all fancy, but serves as an example app
for the [Tor hidden service buildpack](https://github.com/apg/buildpack-tor-hidden-service).

## Setup

```bash
$ git clone https://github.com/apg/hidden-polls
$ cd hidden-polls/
$ heroku create
$ heroku buildpacks:add heroku/go
$ heroku buildpacks:add https://github.com/apg/heroku-buildpack-tor.git
$ heroku addons:create heroku-postgresql:hobby-basic
$ heroku pg:psql < schema/schema.sql
$ heroku pg:psql
app-name => INSERT INTO polls(name, is_open, created_at) ('Example poll', true, NOW());
app-name => INSERT INTO choices(poll_id, answer, created_at) (1, 'Answer 1', NOW());
app-name => INSERT INTO choices(poll_id, answer, created_at) (1, 'Answer 2', NOW());
app-name => INSERT INTO choices(poll_id, answer, created_at) (1, 'Answer 3', NOW());
app-name => INSERT INTO choices(poll_id, answer, created_at) (1, 'Answer 4', NOW());
# If you have a HIDDEN_PRIVATE_KEY and HIDDEN_DOT_ONION, set those now:
$ heroku config:set HIDDEN_PRIVATE_KEY=<YOUR HIDDEN KEY>
$ heroku config:set HIDDEN_DOT_ONION=<YOUR DOT ONION>
# If you don't have them yet... push to heroku. You need an initial build such that you have the tor binary in the slug.
$ git push heroku master
$ heroku run bash
# perform the steps in the README here: https://github.com/apg/heroku-buildpack-tor#how-do-you-get-these-variables
$ heroku config:set HIDDEN_PRIVATE_KEY=<YOUR HIDDEN KEY>
$ heroku config:set HIDDEN_DOT_ONION=<YOUR DOT ONION>
$ git push heroku master 
```


## Copyright 2016 Andrew Gwozdziewyczo
