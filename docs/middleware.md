# Ordered middleware

[log/README.md](../log/README.md) and [http/httpserver/README.md](../http/httpserver/README.md) say how to register middleware. This page says why both order it the same way: the order lives on the middleware, the app can override it, ties are allowed and nil means off. The constitution in [openspec/config.yaml](../openspec/config.yaml) makes the same shape the rule for any group whose members run in order. What's specific to each chain is in [log.md](log.md) and [http.md](http.md).

## Registration order means nothing

Both modules collect their middleware from an fx value group, and dig doesn't guarantee the order of a group's members: it shuffles them on purpose, so nothing comes to rely on registration order. Whatever decides the order has to travel with each member.

## The order lives on the middleware, and the app can override it

A middleware that cares where it runs declares `Order() int`; lower runs first, nearest the caller. One that doesn't counts as 0. The author knows the sensible default (a sampler belongs before an attribute injector, recovery belongs outermost), so the default sits with the code, and a module can export middleware that lands in the right place without the app knowing its number.

The app knows the whole composition, so it has the last word: `Ordered(n, m)` runs `m` at `n`, replacing whatever `m` declares. That covers middleware the app didn't write, a function adapted without an order, and the rare composition where an author's default is wrong. `Ordered` of an `Ordered` replaces the inner order rather than wrapping it, so a value is never more than one wrapper deep.

An order given only at registration was the other candidate, and `log` used it first. It makes the app pick every number, even for middleware whose place is obvious to its author, and authors ended up shipping a suggested-order constant beside their middleware anyway: an author default with extra steps.

## Equal orders don't fail startup

Middleware with equal orders run in an unspecified relative order, which can change from one start to the next. Most middleware doesn't care where it sits, and failing on every tie would force an order onto all of it. A tie-break by type name would hide the shuffle without making the order mean anything. Middleware passed in one slice, outside fx, keeps its given order between equal orders, since a stable sort costs nothing there; across a group nothing can be promised.

## Nil means off

A nil middleware, a nil pointer, func, map, slice or channel behind one, or either wrapped by `Ordered`, is left out when the chain is built. Conditional middleware is common, and its condition usually comes from config known only inside a constructor, after the app has decided which constructors to list. fx keeps a nil in a group rather than dropping it, so without the filter it would fail on every request or record. Failing startup on a nil makes "off" impossible to say without a pass-through middleware, which costs a hop each time and hides the intent. The check uses `reflect`, once, when the chain is built.

