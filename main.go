package main

import (
	"log"

	"github.com/ansrivas/fiberprometheus/v2"
	"github.com/gocolly/colly"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/etag"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/helmet/v2"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"

	"github.com/jufabeck2202/piScraper/internal/adaptors"
	"github.com/jufabeck2202/piScraper/internal/core/domain"
	"github.com/jufabeck2202/piScraper/internal/core/ports"
	"github.com/jufabeck2202/piScraper/internal/core/services/alertsrv"
	"github.com/jufabeck2202/piScraper/internal/core/services/captchasrv"
	"github.com/jufabeck2202/piScraper/internal/core/services/mailsrv"
	"github.com/jufabeck2202/piScraper/internal/core/services/notificationsrv"
	"github.com/jufabeck2202/piScraper/internal/core/services/validatesrv"
	"github.com/jufabeck2202/piScraper/internal/core/services/websitesrv"
	"github.com/jufabeck2202/piScraper/internal/handlers"
	"github.com/jufabeck2202/piScraper/internal/repositories/platforms/pushover"
	"github.com/jufabeck2202/piScraper/internal/repositories/platforms/webhook.go"
	"github.com/jufabeck2202/piScraper/internal/repositories/redis"
)

var (
	redisRepository     ports.RedisRepository
	websiteService      ports.WebsiteService
	alertService        ports.AlertService
	notificationService ports.NotificationService
	mailService         ports.MailService
	captchaService      ports.CaptchaService
	validateService     ports.ValidateService
)

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("No .env file found", err)
	}

	// Initialize the app
	redisRepository, err = redis.NewRedisRepository()
	if err != nil {
		panic("Could not connect to redis")
	}
	websiteService = websitesrv.New(redisRepository)
	alertService = alertsrv.New(redisRepository, websiteService)
	mailService = mailsrv.New(redisRepository)
	captchaService, err = captchasrv.New()
	if err != nil {
		panic("Could not connect to captcha service")
	}
	validateService = validatesrv.New()

	// logEnv()
	go startScraper()

	app := fiber.New()
	app.Use(helmet.New())
	app.Use(compress.New())
	app.Use(etag.New())
	app.Use(favicon.New())
	app.Use(logger.New())
	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(cors.New())
	// Used for local testing
	// app.Use(cors.New(cors.Config{
	// 	AllowOrigins: "http://localhost:3000",
	// 	AllowHeaders: "Origin, Content-Type, Accept",
	// }))
	//Monitoring
	prometheus := fiberprometheus.New("pi-stock-de")
	prometheus.RegisterAt(app, "/metrics")
	app.Use(prometheus.Middleware)

	//controllers
	getController := handlers.NewGetHandler(websiteService)
	alertController := handlers.NewAlertHandler(websiteService, validateService, captchaService, alertService, mailService)
	deleteController := handlers.NewDeleteHandler(websiteService, validateService, captchaService, alertService)
	rssController := handlers.NewRssHandler(websiteService)
	verifyController := handlers.NewVerifMailHandler(mailService)
	unsubscribeController := handlers.NewUnsubscribeMailHandler(alertService, mailService)

	//routes
	app.Static("/", "./frontend/build", fiber.Static{
		CacheDuration: 0,
		MaxAge:        0,
	})
	app.Static("/verify/*", "./frontend/build", fiber.Static{
		CacheDuration: 0,
		MaxAge:        0,
	})
	app.Static("/unsubscribe/*", "./frontend/build", fiber.Static{
		CacheDuration: 0,
		MaxAge:        0,
	})
	app.Get("/api/v1/status", getController.Get)
	app.Get("/rss", rssController.Get)
	app.Get("/api/v1/verify/:email", verifyController.Get)
	app.Get("/api/v1/unsubscribe/:email", unsubscribeController.Get)
	app.Post("/api/v1/alert", alertController.Post)
	app.Delete("/api/v1/alert/", deleteController.Delete)
	app.Listen(":3001")
}

func startScraper() {
	pushoverServie := pushover.NewPushover()
	webhookService := webhook.NewWebhook()
	notificationService = notificationsrv.NewNotificationService(mailService, pushoverServie, webhookService)
	c := cron.New()
	searchPi(true)
	log.Println("Starting cron job")

	c.AddFunc("*/1 * * * *", func() {
		searchPi(false)
	})
	c.Start()
}

func searchPi(firstRun bool) {
	adaptorsList := make([]ports.Adaptor, 0)
	websiteService.Load()
	c := colly.NewCollector(
		colly.Async(true),
	)
	adaptorsList = append(adaptorsList, adaptors.NewBechtle(c, websiteService), adaptors.NewRappishop(c, websiteService), adaptors.NewOkdo(c, websiteService), adaptors.NewBerryBase(c, websiteService), adaptors.NewSemaf(c, websiteService), adaptors.NewBuyZero(c, websiteService), adaptors.NewELV(c, websiteService), adaptors.NewWelectron(c, websiteService), adaptors.NewPishop(c, websiteService), adaptors.NewFunk24(c, websiteService), adaptors.NewReichelt(c, websiteService))
	for _, site := range adaptorsList {
		site.Run()
	}

	for _, site := range adaptorsList {
		site.Wait()
	}

	if !firstRun {
		changes := websiteService.CheckForChanges()
		if len(changes) > 0 {
			log.Println("Found Updates: ", len(changes))
			scheduleUpdates(changes)
		}
	}
	websiteService.Save()
}

func scheduleUpdates(websites domain.Websites) {
	tasksToSchedule := make([]domain.AlertTask, len(websites))

	for _, w := range websites {
		log.Printf("%s changed \n", w.URL)
		alert := alertService.LoadAlerts(w.URL)
		for _, t := range alert {
			tasksToSchedule = append(tasksToSchedule, domain.AlertTask{Website: w, Recipient: t.Recipient, Destination: t.Destination})
			log.Printf("scheduling update for %s and %s \n", w.URL, t.Recipient)
		}
	}
	log.Println("Found websites to update: ", len(tasksToSchedule))
	notificationService.Notifiy(tasksToSchedule)
}
