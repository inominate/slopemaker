# Slope Maker #

Slopemaker is a tool for creating slopes signaling the demise of your 
corporation.

Really it's a tool for handling the removal of inactive users. It will break 
down the tasks of stripping roles and removing users into bite sized chunks 
that can be worked on by multiple people simultaneously without conflict.
When opened in the in-game browser, it uses the IGB javascript to open the 
relevant character's information to accellerate the process.

Supercapital pilots are automatically excluded from this process. Other 
characters can be excluded on the basis of roles, or by name.

Optionally the tool can also compare in-game membership with an external
source, allowing the removal of forgotten unregistered characters. This can be
implemented either via direct database queries or by an external tool exposing
a list of valid characters at a specified URL.


### Installation: ###
```
go get -u github.com/inominate/slopemaker
```

Setup is straightforward, no external database is necessary. Just copy the
purger.conf.example to purger.conf and edit it to fit your needs.
