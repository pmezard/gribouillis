# What is it?

There are plenty of freemium applications letting you draw in a web canvas and save your masterpieces. It is much harder to come by a working free software self-hosted version which does not pull heaps of NodeJS brokenness. Fortunately, very nice folks have written a beautiful client-side drawing canvas widget named `literallycanvas`:

<https://github.com/literallycanvas/literallycanvas>

The only missing part was a small backend to save the drawings and expose them. Here comes `gribouillis` (French for scrawl).


# Setup

From a working Go environment:
```
go get github.com/pmezard/gribouillis
```

Then in your server `appdir/` directory, copy the `literallycanvas/`
subdirectory and the Go binary.

```
cd appdir
./gribouillis -http :5000
```

And voilà, here it is on port 5000. See --help for more options.
