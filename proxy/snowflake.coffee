###
A Coffeescript WebRTC snowflake proxy

Uses WebRTC from the client, and Websocket to the server.

Assume that the webrtc client plugin is always the offerer, in which case
this proxy must always act as the answerer.

TODO: More documentation
###

# Minimum viable snowflake for now - just 1 client.
class Snowflake
  relayAddr:  null
  rateLimit:  null
  pollInterval: null
  retries:    0

  # Janky state machine
  @MODE:
    INIT:              0
    WEBRTC_CONNECTING: 1
    WEBRTC_READY:      2

  @MESSAGE:
    CONFIRMATION: 'You\'re currently serving a Tor user via Snowflake.'

  # Prepare the Snowflake with a Broker (to find clients) and optional UI.
  constructor: (@config, @ui, @broker) ->
    @state = Snowflake.MODE.INIT
    @proxyPairs = []

    if undefined == @config.rateLimitBytes
      @rateLimit = new DummyRateLimit()
    else
      @rateLimit = new BucketRateLimit(
        @config.rateLimitBytes * @config.rateLimitHistory,
        @config.rateLimitHistory
      )
    @retries = 0

  # Set the target relay address spec, which is expected to be websocket.
  # TODO: Should potentially fetch the target from broker later, or modify
  # entirely for the Tor-independent version.
  setRelayAddr: (relayAddr) ->
    @relayAddr = relayAddr
    log 'Using ' + relayAddr.host + ':' + relayAddr.port + ' as Relay.'
    return true

  # Initialize WebRTC PeerConnection, which requires beginning the signalling
  # process. |pollBroker| automatically arranges signalling.
  beginWebRTC: ->
    @state = Snowflake.MODE.WEBRTC_CONNECTING
    log 'ProxyPair Slots: ' + @proxyPairs.length
    log 'Snowflake IDs: ' + (@proxyPairs.map (p) -> p.id).join ' | '
    @pollBroker()
    @pollInterval = setInterval((=> @pollBroker()),
      config.defaultBrokerPollInterval)

  # Regularly poll Broker for clients to serve until this snowflake is
  # serving at capacity, at which point stop polling.
  pollBroker: ->
    # Poll broker for clients.
    pair = @nextAvailableProxyPair()
    if !pair
      log 'At client capacity.'
      # Do nothing until a new proxyPair is available.
      return
    pair.active = true
    msg = 'Polling for client ... '
    msg += '[retries: ' + @retries + ']' if @retries > 0
    @ui.setStatus msg
    recv = @broker.getClientOffer pair.id
    recv.then (desc) =>
      if pair.running
        if !@receiveOffer pair, desc
          pair.active = false
      else
        pair.active = false
    , (err) ->
      pair.active = false
    @retries++


  # Returns the first ProxyPair that's available to connect.
  nextAvailableProxyPair: ->
    if @proxyPairs.length < @config.connectionsPerClient
      return @makeProxyPair @relayAddr
    return @proxyPairs.find (pp, i, arr) -> return !pp.active

  # Receive an SDP offer from some client assigned by the Broker,
  # |pair| - an available ProxyPair.
  receiveOffer: (pair, desc) =>
    try
      offer = JSON.parse desc
      dbg 'Received:\n\n' + offer.sdp + '\n'
      sdp = new SessionDescription offer
      if pair.receiveWebRTCOffer sdp
        @sendAnswer pair
        return true
      else
        return false
    catch e
      log 'ERROR: Unable to receive Offer: ' + e
      return false

  sendAnswer: (pair) ->
    next = (sdp) ->
      dbg 'webrtc: Answer ready.'
      pair.pc.setLocalDescription sdp
    fail = ->
      dbg 'webrtc: Failed to create Answer'
    pair.pc.createAnswer()
    .then next
    .catch fail

  makeProxyPair: (relay) ->
    pair = new ProxyPair relay, @rateLimit, @config.pcConfig
    @proxyPairs.push pair
    pair.onCleanup = (event) =>
      # Delete from the list of active proxy pairs.
      ind = @proxyPairs.indexOf(pair)
      if ind > -1 then @proxyPairs.splice(ind, 1)
    pair.begin()
    return pair

  # Stop all proxypairs.
  disable: ->
    log 'Disabling Snowflake.'
    clearInterval(@pollInterval)
    while @proxyPairs.length > 0
      @proxyPairs.pop().close()
