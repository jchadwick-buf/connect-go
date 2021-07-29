package rerpc

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/akshayjshah/rerpc/internal/statuspb/v0"
)

var (
	// Always advertise that reRPC accepts gzip compression.
	acceptEncodingValue    = strings.Join([]string{CompressionGzip, CompressionIdentity}, ",")
	acceptPostValueDefault = strings.Join(
		[]string{TypeDefaultGRPC, TypeProtoGRPC, TypeJSON},
		",",
	)
	acceptPostValueWithoutJSON = strings.Join(
		[]string{TypeDefaultGRPC, TypeProtoGRPC},
		",",
	)
)

type handlerCfg struct {
	DisableGzipResponse bool
	DisableJSON         bool
	MaxRequestBytes     int
	Registrar           *Registrar
	Interceptor         HandlerInterceptor
	Header              *http.Header
}

// A HandlerOption configures a Handler.
//
// In addition to any options grouped in the documentation below, remember that
// Registrars, Chains, and Options are also valid HandlerOptions.
type HandlerOption interface {
	applyToHandler(*handlerCfg)
}

type serveJSONOption struct {
	Disable bool
}

func (o *serveJSONOption) applyToHandler(cfg *handlerCfg) {
	cfg.DisableJSON = o.Disable
}

// ServeJSON enables or disables support for JSON requests and responses.
//
// By default, handlers support JSON.
func ServeJSON(enable bool) HandlerOption {
	return &serveJSONOption{!enable}
}

// A Handler is the server-side implementation of a single RPC defined by a
// protocol buffer service. It's the interface between the reRPC library and
// the code generated by the reRPC protoc plugin; most users won't ever need to
// deal with it directly.
//
// To see an example of how Handler is used in the generated code, see the
// internal/pingpb/v0 package.
type Handler struct {
	methodFQN      string
	implementation UnaryHandler
	// rawGRPC is used only for our hand-rolled reflection handler, which needs
	// bidi streaming
	rawGRPC func(
		http.ResponseWriter,
		*http.Request,
		string, // request compression
		string, // response compression
	)
	config handlerCfg
}

// NewHandler constructs a Handler.
func NewHandler(
	methodFQN string, // fully-qualified protobuf method name
	impl func(context.Context, proto.Message) (proto.Message, error),
	opts ...HandlerOption,
) *Handler {
	var cfg handlerCfg
	for _, opt := range opts {
		opt.applyToHandler(&cfg)
	}
	if reg := cfg.Registrar; reg != nil {
		reg.register(methodFQN)
	}
	return &Handler{
		methodFQN:      methodFQN,
		implementation: impl,
		config:         cfg,
	}
}

// Serve executes the handler, much like the standard library's http.Handler.
// Unlike http.Handler, it requires a pointer to the protoc-generated request
// struct. See the internal/pingpb/v0 package for an example of how this code
// is used in reRPC's generated code.
//
// As long as the caller allocates a new request struct for each call, this
// method is safe to use concurrently.
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request, req proto.Message) {
	// To ensure that we can re-use connections, always consume and close the
	// request body.
	defer r.Body.Close()
	defer io.Copy(ioutil.Discard, r.Body)

	if r.Method != http.MethodPost {
		// grpc-go returns a 500 here, but interoperability with non-gRPC HTTP
		// clients is better if we return a 405.
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	spec := &Specification{
		Method:              h.methodFQN,
		ContentType:         r.Header.Get("Content-Type"),
		RequestCompression:  CompressionIdentity,
		ResponseCompression: CompressionIdentity,
	}
	if spec.ContentType == TypeJSON && h.config.DisableJSON {
		w.Header().Set("Accept-Post", acceptPostValueWithoutJSON)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	if ct := spec.ContentType; ct != TypeDefaultGRPC && ct != TypeProtoGRPC && ct != TypeJSON {
		// grpc-go returns 500, but the spec recommends 415.
		// https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md#requests
		w.Header().Set("Accept-Post", acceptPostValueDefault)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}

	// We need to parse metadata before entering the interceptor stack, but we'd
	// like any errors we encounter to be visible to interceptors for
	// observability. We'll collect any such errors here and use them to
	// short-circuit early later on.
	//
	// NB, future refactorings will need to take care to avoid typed nils here.
	var failed *Error

	timeout, err := parseTimeout(r.Header.Get("Grpc-Timeout"))
	if err != nil && err != errNoTimeout {
		// Errors here indicate that the client sent an invalid timeout header, so
		// the error text is safe to send back.
		failed = wrap(CodeInvalidArgument, err)
	} else if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		r = r.WithContext(ctx)
	} // else err == errNoTimeout, nothing to do

	if spec.ContentType == TypeJSON {
		if r.Header.Get("Content-Encoding") == "gzip" {
			spec.RequestCompression = CompressionGzip
		}
		// TODO: Actually parse Accept-Encoding instead of this hackery.
		if !h.config.DisableGzipResponse && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			spec.ResponseCompression = CompressionGzip
		}
	} else {
		spec.RequestCompression = CompressionIdentity
		if me := r.Header.Get("Grpc-Encoding"); me != "" {
			switch me {
			case CompressionIdentity:
				spec.RequestCompression = CompressionIdentity
			case CompressionGzip:
				spec.RequestCompression = CompressionGzip
			default:
				// Per https://github.com/grpc/grpc/blob/master/doc/compression.md, we
				// should return CodeUnimplemented and specify acceptable compression(s)
				// (in addition to setting the Grpc-Accept-Encoding header).
				if failed == nil {
					failed = errorf(
						CodeUnimplemented,
						"unknown compression %q: accepted grpc-encoding values are %v",
						me, acceptEncodingValue,
					)
				}
			}
		}
		// Follow https://github.com/grpc/grpc/blob/master/doc/compression.md.
		// (The grpc-go implementation doesn't read the "grpc-accept-encoding" header
		// and doesn't support compression method asymmetry.)
		spec.ResponseCompression = spec.RequestCompression
		if h.config.DisableGzipResponse {
			spec.ResponseCompression = CompressionIdentity
		} else if mae := r.Header.Get("Grpc-Accept-Encoding"); mae != "" {
			for _, enc := range strings.FieldsFunc(mae, splitOnCommasAndSpaces) {
				switch enc {
				case CompressionGzip: // prefer gzip
					spec.ResponseCompression = CompressionGzip
					break
				case CompressionIdentity:
					spec.ResponseCompression = CompressionIdentity
					break
				}
			}
		}
	}

	// We may write to the body in the implementation (e.g., reflection handler), so we should
	// set headers here.
	w.Header().Set("Content-Type", spec.ContentType)
	if spec.ContentType != TypeJSON {
		w.Header().Set("Grpc-Accept-Encoding", acceptEncodingValue)
		w.Header().Set("Grpc-Encoding", spec.ResponseCompression)
		// Every gRPC response will have these trailers.
		w.Header().Add("Trailer", "Grpc-Status")
		w.Header().Add("Trailer", "Grpc-Message")
		w.Header().Add("Trailer", "Grpc-Status-Details-Bin")
	}

	ctx := NewHandlerContext(r.Context(), *spec, r.Header, w.Header())
	var implementation UnaryHandler
	if failed != nil {
		implementation = UnaryHandler(func(context.Context, proto.Message) (proto.Message, error) {
			return nil, failed
		})
	} else if spec.ContentType == TypeJSON {
		implementation = h.implementationJSON(w, r, spec)
	} else {
		implementation = h.implementationGRPC(w, r, spec)
	}
	res, err := h.wrap(implementation)(ctx, req)
	if err := h.writeResult(r.Context(), w, spec, res, err); err != nil {
		// TODO: observability
	}
}

func (h *Handler) implementationJSON(w http.ResponseWriter, r *http.Request, spec *Specification) UnaryHandler {
	return UnaryHandler(func(ctx context.Context, req proto.Message) (proto.Message, error) {
		var body io.Reader = r.Body
		if spec.RequestCompression == CompressionGzip {
			gr, err := gzip.NewReader(body)
			if err != nil {
				return nil, errorf(CodeInvalidArgument, "can't read gzipped body")
			}
			defer gr.Close()
			body = gr
		}
		if max := h.config.MaxRequestBytes; max > 0 {
			body = &io.LimitedReader{
				R: body,
				N: int64(max),
			}
		}
		if err := unmarshalJSON(body, req); err != nil {
			return nil, errorf(CodeInvalidArgument, "can't unmarshal JSON body")
		}
		return h.implementation(ctx, req)
	})
}

func (h *Handler) implementationGRPC(w http.ResponseWriter, r *http.Request, spec *Specification) UnaryHandler {
	return UnaryHandler(func(ctx context.Context, req proto.Message) (proto.Message, error) {
		if raw := h.rawGRPC; raw != nil {
			raw(w, r, spec.RequestCompression, spec.ResponseCompression)
			return nil, nil
		}
		if err := unmarshalLPM(r.Body, req, spec.RequestCompression, h.config.MaxRequestBytes); err != nil {
			return nil, errorf(CodeInvalidArgument, "can't unmarshal protobuf body")
		}
		return h.implementation(ctx, req)
	})
}

func (h *Handler) writeResult(ctx context.Context, w http.ResponseWriter, spec *Specification, res proto.Message, err error) error {
	if spec.ContentType == TypeJSON {
		return h.writeResultJSON(ctx, w, spec, res, err)
	}
	return h.writeResultGRPC(ctx, w, spec, res, err)
}

func (h *Handler) writeResultJSON(ctx context.Context, w http.ResponseWriter, spec *Specification, res proto.Message, err error) error {
	// Even if the client requested gzip compression, check Content-Encoding to
	// make sure some other HTTP middleware hasn't already swapped out the
	// ResponseWriter.
	if spec.ResponseCompression == CompressionGzip && w.Header().Get("Content-Encoding") == "" {
		w.Header().Set("Content-Encoding", "gzip")
		gw := gzWriterPool.Get().(*gzip.Writer)
		gw.Reset(w)
		w = &gzipResponseWriter{ResponseWriter: w, gw: gw}
		defer func() {
			gw.Close()           // close if we haven't already
			gw.Reset(io.Discard) // don't keep references
			gzWriterPool.Put(gw)
		}()
	}
	if err != nil {
		return writeErrorJSON(w, err)
	}
	return marshalJSON(w, res)
}

func (h *Handler) writeResultGRPC(ctx context.Context, w http.ResponseWriter, spec *Specification, res proto.Message, err error) error {
	if err != nil {
		writeErrorGRPC(w, err)
		return nil
	}
	if err := marshalLPM(w, res, spec.ResponseCompression, 0 /* maxBytes */); err != nil {
		// It's safe to write gRPC errors even after we've started writing the
		// body.
		writeErrorGRPC(w, errorf(CodeUnknown, "can't marshal protobuf response"))
		return err
	}
	writeErrorGRPC(w, nil)
	return nil
}

func (h *Handler) wrap(uh UnaryHandler) UnaryHandler {
	if h.config.Interceptor != nil {
		return h.config.Interceptor.WrapHandler(uh)
	}
	return uh
}

func splitOnCommasAndSpaces(c rune) bool {
	return c == ',' || c == ' '
}

func writeErrorJSON(w http.ResponseWriter, err error) error {
	s := statusFromError(err)
	bs, err := jsonpbMarshaler.Marshal(s)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"code": %d, "message": "error marshaling status with code %d"}`, CodeInternal, s.Code)
		return err
	}
	w.WriteHeader(Code(s.Code).http())
	_, err = w.Write(bs)
	return err
}

func writeErrorGRPC(w http.ResponseWriter, err error) {
	if err == nil {
		w.Header().Set("Grpc-Status", strconv.Itoa(int(CodeOK)))
		w.Header().Set("Grpc-Message", "")
		w.Header().Set("Grpc-Status-Details-Bin", "")
		return
	}
	// gRPC errors are successes at the HTTP level and net/http automatically
	// sends a 200 if we don't set a status code. Leaving the HTTP status
	// implicit lets us use this function when we hit an error partway through
	// writing the body.
	s := statusFromError(err)
	code := strconv.Itoa(int(s.Code))
	// If we ever need to send more trailers, make sure to declare them in the headers
	// above.
	if bin, err := proto.Marshal(s); err != nil {
		w.Header().Set("Grpc-Status", strconv.Itoa(int(CodeInternal)))
		w.Header().Set("Grpc-Message", percentEncode("error marshaling protobuf status with code "+code))
	} else {
		w.Header().Set("Grpc-Status", code)
		w.Header().Set("Grpc-Message", percentEncode(s.Message))
		w.Header().Set("Grpc-Status-Details-Bin", encodeBinaryHeader(bin))
	}
}

func statusFromError(err error) *statuspb.Status {
	s := &statuspb.Status{
		Code:    int32(CodeUnknown),
		Message: err.Error(),
	}
	if re, ok := AsError(err); ok {
		s.Code = int32(re.Code())
		s.Details = re.Details()
		if e := re.Unwrap(); e != nil {
			s.Message = e.Error() // don't repeat code
		}
	}
	return s
}