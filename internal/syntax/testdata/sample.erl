%% A bounded cache of recent keys.
-module(cache).
-behaviour(gen_server).

-export([start_link/1, push/2, keys/1]).
-export([init/1, handle_call/3, handle_cast/2]).

-define(CAPACITY, 16).
-define(MASK, 16#FF).
-define(TIMEOUT, 5000).

-record(state, {slots = [], capacity = ?CAPACITY}).

start_link(Opts) ->
    gen_server:start_link({local, ?MODULE}, ?MODULE, Opts, []).

push(Pid, Key) ->
    gen_server:cast(Pid, {push, Key}).

keys(Pid) ->
    gen_server:call(Pid, keys, ?TIMEOUT).

init([]) ->
    {ok, #state{}}.

handle_call(keys, _From, State = #state{slots = Slots}) ->
    {reply, Slots, State}.

handle_cast({push, Key}, State = #state{slots = Slots, capacity = Cap}) ->
    Trimmed = lists:sublist([Key | Slots], Cap),
    {noreply, State#state{slots = Trimmed}}.
