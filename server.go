package letsrest

import (
	"encoding/json"
	"github.com/Sirupsen/logrus"
	"github.com/iris-contrib/middleware/logger"
	"github.com/kataras/iris"
	"github.com/speps/go-hashids"
	"golang.org/x/time/rate"
	"net/http"
	"os"
	"strings"
	"time"
)

const MAX_BODY_SIZE = 1 * 1024 * 1024

var log = logrus.New()

func NewServer(r Requester, s RequestStore) *Server {
	log.Out = os.Stdout
	formatter := new(logrus.TextFormatter)
	formatter.ForceColors = true
	log.Formatter = formatter
	log.Level = logrus.DebugLevel

	//anonymLimiter := rate.NewLimiter(rate.Every(time.Duration(200)*time.Millisecond), 5)
	anonymLimiter := rate.NewLimiter(rate.Every(time.Duration(5)*time.Second), 1)

	return &Server{requester: r, store: s, taskCh: make(chan *RequestTask, 100), anonymLimiter: anonymLimiter}
}

type Server struct {
	requester     Requester
	store         RequestStore
	taskCh        chan *RequestTask
	anonymLimiter *rate.Limiter
}

func IrisHandler(requester Requester, store RequestStore) (*iris.Framework, *Server) {
	srv := NewServer(requester, store)
	api := iris.New()
	api.UseFunc(logger.New())

	api.Get("/", func(ctx *iris.Context) {
		ctx.JSON(http.StatusOK, "OK")
		return
	})

	v1 := api.Party("/api/v1")
	{
		v1.Get("/", func(ctx *iris.Context) {
			ctx.JSON(http.StatusOK, "OK")
			return
		})

		// Fire userNotFoundHandler when Not Found
		// inside http://localhost:6111/users/*anything
		//api.OnError(404, userNotFoundHandler)

		requests := v1.Party("/requests")

		requests.Put("", srv.CheckAuthToken, srv.CreateRequest)
		requests.Get("/:id", srv.GetRequest)
		requests.Get("/:id/responses", srv.GetResponse)
		requests.Get("/:id/body", srv.GetResponseBody)
		requests.Get("", srv.GetRequestTaskList)

		v1.Get("/test", srv.Test)
	}

	return api, srv
}

func (s *Server) ListenForTasks() {
	for task := range s.taskCh {
		resp, err := s.requester.Do(task)
		s.store.SetResponse(task.ID, resp, err)
	}
}

func (s *Server) CheckAuthToken(ctx *iris.Context) {
	_, ok := ctx.Request.Header["Authorization"]
	// для неавторизированных пользователей проверяем, что лимит запросов не превышен
	if !ok {
		if !s.anonymLimiter.Allow() {
			ctx.ResponseWriter.Header().Add("Content-Type", "application/json")
			ctx.ResponseWriter.WriteHeader(http.StatusTooManyRequests)
			ctx.ResponseWriter.Write([]byte("You have reached maximum request limit."))
			ctx.StopExecution()
			return
		}

		authToken, err := hashids.New().Encode([]int{time.Now().Second()})
		Must(err, "hashids.New().Encode([]int{time.Now().Second()})")
		ctx.ResponseWriter.Header().Add("X-LetsRest-AuthToken", authToken)
	}
	ctx.Next()
}

func (s *Server) CreateRequest(ctx *iris.Context) {
	requestTask := &RequestTask{}

	err := ctx.Request.ParseMultipartForm(MAX_BODY_SIZE)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, err.Error())
		log.WithError(err).Debug("Parse form")
		return
	}

	requestTaskJson := ctx.FormValue("requestTask")
	if err := json.Unmarshal([]byte(requestTaskJson), requestTask); err != nil {
		ctx.JSON(http.StatusBadRequest, err.Error())
		log.WithError(err).Error("Unmarshal request task")
		return
	}

	file, _, err := ctx.FormFile("fileBody")
	if err == nil && file != nil {
		var data []byte
		_, err := file.Read(data)
		if err != nil {
			log.WithError(err).Error("Error reading fileBody")
			ctx.JSON(http.StatusBadRequest, err.Error())
			return
		}
		requestTask.Body = data
	}

	requestTask, err = s.store.Save(requestTask)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, err.Error())
		return
	}

	s.taskCh <- requestTask

	ctx.JSON(http.StatusCreated, requestTask)
}

func (s *Server) GetRequest(ctx *iris.Context) {
	cReq, err := s.store.Get(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, err.Error())
		return
	}

	if cReq == nil {
		ctx.JSON(http.StatusNotFound, RequestNotFoundResponse(ctx.Param("id")))
		return
	}

	ctx.JSON(http.StatusOK, cReq)
}

func (s *Server) GetResponse(ctx *iris.Context) {
	cResp, err := s.store.GetResponse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, err.Error())
		return
	}

	if cResp == nil {
		ctx.JSON(http.StatusNotFound, RequestNotFoundResponse(ctx.Param("id")))
		return
	}

	if cResp.Status.Status == "in_progress" {
		ctx.JSON(http.StatusPartialContent, cResp)
		return
	}

	ctx.JSON(http.StatusOK, cResp)
}

func (s *Server) GetResponseBody(ctx *iris.Context) {
	cResp, err := s.store.GetResponse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, err.Error())
		return
	}

	if cResp == nil {
		ctx.JSON(http.StatusNotFound, RequestNotFoundResponse(ctx.Param("id")))
		return
	}

	if cResp.Status.Status == "in_progress" {
		ctx.JSON(http.StatusPartialContent, cResp)
		return
	}

	h := findHeader("Content-Type", cResp.Response.Headers)

	if h != nil {
		ctx.ResponseWriter.Header().Add("Content-Type", h.Value)
	} else {
		ctx.ResponseWriter.Header().Add("Content-Type", "application/octet-stream")
	}
	ctx.ResponseWriter.WriteHeader(http.StatusOK)
	ctx.ResponseWriter.Write(cResp.Response.Body)
}

func (s *Server) GetRequestTaskList(ctx *iris.Context) {
	taskList, err := s.store.List()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, err.Error())
		return
	}
	ctx.JSON(http.StatusOK, taskList)
}

func findHeader(name string, headers []Header) *Header {
	loweredName := strings.ToLower(name)
	for _, header := range headers {
		if strings.ToLower(header.Name) == loweredName {
			return &header
		}
	}
	return nil
}

func (s *Server) Test(ctx *iris.Context) {
	ctx.JSON(http.StatusOK, ctx.Request.URL.String())
}
