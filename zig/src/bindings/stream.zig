//! VT parsing, effects, clipboard requests and standalone OSC parsing.
const p = @import("policy.zig");
const zigo = p.zigo;
const api = p.api;

// The `implements` feature on `feed` is what makes this true; the assertion is
// what keeps it true.
pub const Stream = api.handle("Stream", .{ .fields = &.{
    .{ .path = "inner.handler.semantic_failure", .name = "failed", .doc = "True once a sequence failed in a way the terminal could not absorb, such as an allocation failure. Streams are best-effort and keep going." },
} }).use(p.satisfies.plugin, .{ .interfaces = &.{"io.WriteCloser"} }).with(.{ .doc =
    \\Stream parses VT bytes into its parent terminal. Serialize complete calls,
    \\including event iteration, with all other calls touching the parent or its
    \\children. Callbacks run synchronously; only request-response operations may
    \\reenter the active terminal's bindings. Close the stream before its parent
    \\terminal.
}).context();

const ClipboardRequestDecl = api.handle("ClipboardRequest", .{});
const ClipboardRequest = p.convenience.clipboardReply(
    ClipboardRequestDecl,
    api,
    ClipboardRequestDecl.context(),
).context();

// Every payload the parser exposes is a field it filled during `end`, so the
// accessors are paths rather than fifteen bodies that would each return one
// field. The documentation moves here with them: a field accessor takes its
// Go doc from `.doc` and nothing else.
const OSCParser = api.handle("OSCParser", .{
    .fields = &.{
        .{ .path = "kind", .name = "command", .doc = "The last command parsed, or `invalid` if `end` has not said yes." },
        .{ .path = "window_title", .name = "windowTitle", .doc = "OSC 0 and OSC 2: the window title. ghostty does not decode it. Under title mode 0 the bytes are hex, and otherwise they are UTF-8 or latin1 depending on how the terminal was set up, which only the embedder knows." },
        .{ .path = "icon_name", .name = "icon", .doc = "OSC 1: the icon name. Not well defined by any specification, and ghostty itself ignores it." },
        .{ .path = "pwd_value", .name = "pwd", .doc = "OSC 7: the shell's working directory, as a `file://` URL. ghostty does not check that it is one, and neither does this." },
        .{ .path = "hyperlink_uri", .name = "hyperlinkURI", .doc = "OSC 8: the link target." },
        .{ .path = "hyperlink_id", .name = "hyperlinkID", .doc = "OSC 8: the link's `id=` parameter, empty when it carried none. Two runs sharing an id are one link even when they are not adjacent." },
        .{ .path = "notification_title", .name = "notificationTitle", .doc = "OSC 9 and OSC 777: the notification title." },
        .{ .path = "notification_body", .name = "notificationBody", .doc = "OSC 9 and OSC 777: the notification body." },
        // OSC 52 carries its payload base64-encoded and this parser hands it over
        // as it arrived, so what comes back is that encoding rather than the bytes
        // inside it. `ClipboardRequest.contentData` is the decoded form.
        .{ .path = "clipboard_data", .name = "clipboardData", .doc = "OSC 52: the base64 payload to put on the clipboard, or a bare `?` when the program is asking to read the clipboard rather than to write it." },
        .{ .path = "clipboard_kind", .name = "clipboardSelection", .doc = "OSC 52: which selection the request names, as the protocol's own character (`c` for clipboard, `p` for primary, `s` for the configured default), or zero for any other command." },
        .{ .path = "mouse_shape", .name = "mouseShape", .doc = "OSC 22: the pointer shape, usually a W3C CSS cursor name. ghostty parses whatever string is given without validating it." },
        .{ .path = "prompt_action", .name = "semanticPromptAction", .doc = "OSC 133: which prompt boundary this sequence marks." },
        .{ .path = "prompt_options", .name = "semanticPromptOptions", .doc = "OSC 133: the raw, unvalidated option string that followed the action." },
        .{ .path = "progress_state", .name = "progressState", .doc = "OSC 9;4: what the program says about its progress." },
        .{ .path = "progress", .name = "progressValue", .doc = "OSC 9;4: percent complete, or -1 when the report carried no percentage." },
    },
}).context();

const stream_group = Stream.members(p.functions(api, .{
    api.func("freeStream", .{ .role = .{ .destructor = Stream.typeRef() } }),
    Stream.func("atGround", .{}),
    Stream.func("feedUntilGround", .{ .params = &.{p.bytesArg(1)} }),
    // The writer adapter makes fmt.Fprintf(stream, ...) feed VT bytes, and the
    // string_writer beside it keeps io.WriteString off the copy path. `feed`
    // takes bytes rather than text -- what arrives from a pty is not UTF-8 by
    // contract -- so the string wrapper lends the string's bytes for the length
    // of the call rather than converting them. Both call the same native
    // `feed`, which stays unexported: `Write` and `WriteString` are the whole
    // public surface, so a stream is fed the way any `io.Writer` is.
    Stream.func("feed", .{ .params = &.{p.bytesArg(1)} })
        .use(zigo.features.implements, .{ .kinds = &.{ .writer, .string_writer } }),
    Stream.func("nextEventValue", .{ .returns = zigo.result.releasedBy(api.ref("freeBuffer")) })
        .use(zigo.features.iterator, .{ .name = "EventValues" }),
    Stream.func("setUnknownMaxBytes", .{}),
    Stream.func("setVersionReport", .{}),
    Stream.func("setEnquiryResponse", .{}),
    Stream.func("colorSchemeChanged", .{}),
    Stream.func("clearColorScheme", .{}),

    // Kitty drag and drop. The handler is held by this stream and released
    // when it closes. It takes its whole payload as arguments, so there is
    // nothing to read back afterwards -- which also means the acceptance is
    // the one being reported rather than whatever a later event in the same
    // feed replaced it with.
    "onDrag",
    "dragActive",
    "dragRegisteredMimes",
    "dragClientAccepted",
    "dragMove",
    "dragLeave",
    // The dropped payload is a file or an image as often as it is text.
    api.func("dragAddItem", .{ .params = &.{p.bytesArg(2)} }),
    "dragDrop",
    "dragClearItems",

    // A clipboard request runs while `feed` is on the stack, on the thread
    // that called it, and the callback answers it through the request it is
    // handed. Both handlers are held by this stream and released when it
    // closes.
    Stream.func("onClipboardWriteRequest", .{}).with(.{ .doc = "Registers a synchronous clipboard-write handler. A nil callback returns ErrNilCallback without replacing the existing handler." }),
    Stream.func("onClipboardReadRequest", .{}).with(.{ .doc = "Registers a synchronous clipboard-read handler. A nil callback returns ErrNilCallback without replacing the existing handler." }),
    Stream.func("writeContinuation", .{}),
    Stream.func("hasReplies", .{}),
    Stream.func("writeSnapshot", .{}),
    Stream.func("writeReplies", .{}),
}));

const clipboard_group = ClipboardRequest.members(ClipboardRequest.funcs(.{ .names = &.{
    "location",
    "name",
    "granted",
    "canRemember",
    "contentCount",
    "contentMime",
    "mimeCount",
    "mime",
    "allow",
    "replyText",
    "clearReplyContents",
    "replyContents",
    "deny",
} }) ++ &[_]zigo.Entry{
    // What a program asked to put on the clipboard, already decoded out of the
    // OSC 52 base64. Arbitrary bytes at that point, unlike the encoded form
    // `OSCParser.clipboardData` reports.
    // Native signature: (self, injected gpa, MIME text, opaque payload).
    ClipboardRequest.func("addContent", .{ .params = &.{p.bytesArg(3)} }),
    ClipboardRequest.func("contentData", .{ .returns = .{ .semantic = .opaque_bytes } }),
});

// `Stream` needs a `Terminal` behind it; this one parses a sequence and hands
// back what it said, with no terminal state involved at all.
const osc_group = OSCParser.members(&[_]zigo.Entry{
    api.func("newOSCParser", .{ .role = .{ .constructor = .{ .type = OSCParser.typeRef() } } }),
    api.func("freeOSCParser", .{ .role = .{ .destructor = OSCParser.typeRef() } }),
    OSCParser.func("feed", .{ .params = &.{p.bytesArg(1)} }),
} ++ OSCParser.funcs(.{ .names = &.{ "end", "reset" } }));

pub const declarations = [_]zigo.Entry{
    stream_group,
    clipboard_group,
    osc_group,
    p.enumeration("StreamEvent", .{ .text = true }),
    p.enumeration("ProgressState", .{ .text = true }),
    p.enumeration("ColorScheme", .{ .text = true }),
    p.enumeration("DragEvent", .{ .text = true }),
    p.enumeration("DragOperation", .{ .text = true }),
    p.flags("DragOperations", "_pad"),
    api.value("DragMove", .{}),
    p.flags("DragNotice", "_pad"),
    p.callback("DragFn", "DragHandler"),
    p.enumeration("ClipboardLocation", .{ .text = true, .open = true }),
    p.enumeration("ClipboardDenial", .{}),
    p.callback("ClipboardFn", "ClipboardHandler"),
    api.value("FeedBoundary", .{}),
    api.materialized("Event", .{ .fields = &.{.{ .name = "sequence", .semantic = .opaque_bytes }} }),
    p.enumeration("OSCCommand", .{ .text = true }),
    p.enumeration("OSCTerminator", .{ .text = true }),
    p.enumeration("SemanticPromptAction", .{ .text = true }),
};
