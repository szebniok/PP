//> using lib "com.lihaoyi::os-lib:0.7.8"
//> using lib "org.simplejavamail:simple-java-mail:7.5.0"
//> using lib "org.scalikejdbc::scalikejdbc:4.0.0"
//> using lib "org.xerial:sqlite-jdbc:3.39.2.0"

import org.simplejavamail.converter.EmailConverter
import org.simplejavamail.api.email.{Email, Recipient}
import scalikejdbc._
import scala.util.Try

import scala.jdk.CollectionConverters.*

def filesByExtension(
  extension: String,
  dir: os.Path = os.pwd): Seq[os.Path] =
    os.list(dir).filter { f =>
      f.last.endsWith(s".$extension") && os.isFile(f)
    }

Class.forName("org.sqlite.JDBC")
ConnectionPool.singleton("jdbc:sqlite:../mails.sqlite", null, null)

def createTables() = DB autoCommit { implicit session => 
  sql"""CREATE TABLE IF NOT EXISTS unlabeled (
  id INTEGER NOT NULL PRIMARY KEY,
  address_from TEXT,
  address_to TEXT,
  date TEXT,
  subject TEXT,
  text TEXT,
  html TEXT,
  processed INTEGER,
  ignored INTEGER
  ) STRICT""".execute.apply()

  sql"""CREATE TABLE IF NOT EXISTS labeled (
  id INTEGER NOT NULL PRIMARY KEY,
  unlabeled_id INTEGER NOT NULL,
  address_from TEXT,
  address_to TEXT,
  date TEXT,
  subject TEXT,
  text TEXT,
  category TEXT
  ) STRICT""".execute.apply()

  sql"""CREATE TABLE IF NOT EXISTS categories (
  id INTEGER NOT NULL PRIMARY KEY,
  name TEXT UNIQUE
  ) STRICT""".execute.apply()
}
createTables()

def formatRecipient(recipient: Recipient): String =
  if (recipient.getName == null) 
    recipient.getAddress
  else
    s"${recipient.getName} <${recipient.getAddress}>"

def insertMail(e: Email) = DB autoCommit { implicit session =>
  val address_from = formatRecipient(e.getFromRecipient)
  val address_to = e.getRecipients.asScala.toSeq.map(formatRecipient).mkString(", ")
  val text = if (e.getPlainText == null) "" else e.getPlainText
  val html = if (e.getHTMLText == null) "" else e.getHTMLText
  sql"""INSERT INTO unlabeled(
      address_from,
      address_to,
      date,
      subject,
      text,
      html,
      processed,
      ignored
  ) VALUES ($address_from, $address_to, ${e.getSentDate}, ${e.getSubject}, $text, $html, FALSE, FALSE)
  """.update.apply()
}

val directory = os.Path(args.head)
val files = filesByExtension("eml", directory)

val emailFiles = files.map(_.toIO)
val emails = emailFiles.map(f => (f, Try(EmailConverter.emlToEmail(f))))

println(s"Failed to load ${emails.filter(_._2.isFailure).length} emails...")
emails.foreach {case (f, email) =>
  email.fold(ex => println(s"${f.getName}: ${ex.getMessage}"), e => insertMail(e))
}
